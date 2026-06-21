//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteTekshotJobStore struct {
	db *sql.DB
}

func NewSQLiteTekshotJobStore(db *sql.DB) *SQLiteTekshotJobStore {
	return &SQLiteTekshotJobStore{db: db}
}

func (s *SQLiteTekshotJobStore) Create(ctx context.Context, job *store.TekshotJob) (*store.TekshotJob, error) {
	if existing, err := s.GetByExternalJobUUID(ctx, job.ExternalJobUUID); err == nil && existing != nil {
		return existing, nil
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now().UTC()
	if job.Status == "" {
		job.Status = store.TekshotJobQueued
	}
	if job.ProgressMessage == "" {
		job.ProgressMessage = "Queued"
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tekshot_jobs
		(id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, job_type, agent_key, session_key,
		 status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		 attempt_count, locked_until, created_at, updated_at, completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID.String(), job.ExternalJobUUID, job.WorkspaceID, job.WorkspaceUUID, job.ExternalUserID, job.JobType, job.AgentKey, job.SessionKey,
		job.Status, job.ProgressMessage, job.ErrorMessage, string(job.RequestJSON), string(job.ResultJSON), job.CallbackURL, job.CallbackToken,
		job.AttemptCount, timePtrToString(job.LockedUntil), job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano), timePtrToString(job.CompletedAt),
	)
	if err != nil {
		return nil, err
	}
	return s.GetByExternalJobUUID(ctx, job.ExternalJobUUID)
}

func (s *SQLiteTekshotJobStore) Get(ctx context.Context, id uuid.UUID) (*store.TekshotJob, error) {
	return s.scanOne(ctx, selectSQLiteTekshotJobSQL+" WHERE id = ?", id.String())
}

func (s *SQLiteTekshotJobStore) GetByExternalJobUUID(ctx context.Context, externalJobUUID string) (*store.TekshotJob, error) {
	return s.scanOne(ctx, selectSQLiteTekshotJobSQL+" WHERE external_job_uuid = ?", externalJobUUID)
}

func (s *SQLiteTekshotJobStore) ClaimNext(ctx context.Context, lockFor time.Duration) (*store.TekshotJob, error) {
	now := time.Now().UTC()
	lockedUntil := now.Add(lockFor)
	res, err := s.db.ExecContext(ctx, `UPDATE tekshot_jobs
		SET status = ?, progress_message = ?, attempt_count = attempt_count + 1, locked_until = ?, updated_at = ?
		WHERE id = (
			SELECT id FROM tekshot_jobs
			WHERE status = ? AND (locked_until = '' OR locked_until <= ?)
			ORDER BY created_at ASC
			LIMIT 1
		)`,
		store.TekshotJobRunning, "Starting Tekshot job", lockedUntil.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), store.TekshotJobQueued, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	return s.scanOne(ctx, selectSQLiteTekshotJobSQL+" WHERE status = ? AND locked_until = ? ORDER BY updated_at DESC LIMIT 1",
		store.TekshotJobRunning, lockedUntil.Format(time.RFC3339Nano))
}

func (s *SQLiteTekshotJobStore) MarkRunning(ctx context.Context, id uuid.UUID, progress string, lockFor time.Duration) error {
	if progress == "" {
		progress = "Running Tekshot job"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_jobs SET status = ?, progress_message = ?, locked_until = ?, updated_at = ? WHERE id = ?`,
		store.TekshotJobRunning, progress, now.Add(lockFor).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

func (s *SQLiteTekshotJobStore) MarkCompleted(ctx context.Context, id uuid.UUID, result json.RawMessage, progress string) error {
	if progress == "" {
		progress = "Tekshot job completed"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_jobs SET status = ?, progress_message = ?, error_message = '', result_json = ?, locked_until = '', completed_at = ?, updated_at = ? WHERE id = ?`,
		store.TekshotJobCompleted, progress, string(result), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

func (s *SQLiteTekshotJobStore) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_jobs SET status = ?, progress_message = ?, error_message = ?, locked_until = '', completed_at = ?, updated_at = ? WHERE id = ?`,
		store.TekshotJobFailed, "Tekshot job failed", message, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

const selectSQLiteTekshotJobSQL = `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, job_type, agent_key, session_key,
	status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
	attempt_count, locked_until, created_at, updated_at, completed_at
	FROM tekshot_jobs`

func (s *SQLiteTekshotJobStore) scanOne(ctx context.Context, query string, args ...any) (*store.TekshotJob, error) {
	job, err := scanSQLiteTekshotJob(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func scanSQLiteTekshotJob(row interface {
	Scan(dest ...any) error
}) (*store.TekshotJob, error) {
	var job store.TekshotJob
	var id, lockedUntil, createdAt, updatedAt, completedAt string
	var requestJSON, resultJSON string
	if err := row.Scan(
		&id, &job.ExternalJobUUID, &job.WorkspaceID, &job.WorkspaceUUID, &job.ExternalUserID, &job.JobType, &job.AgentKey, &job.SessionKey,
		&job.Status, &job.ProgressMessage, &job.ErrorMessage, &requestJSON, &resultJSON, &job.CallbackURL, &job.CallbackToken,
		&job.AttemptCount, &lockedUntil, &createdAt, &updatedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	job.ID = parsedID
	job.RequestJSON = json.RawMessage(requestJSON)
	job.ResultJSON = json.RawMessage(resultJSON)
	job.LockedUntil = parseTimePtr(lockedUntil)
	job.CreatedAt = parseTime(createdAt)
	job.UpdatedAt = parseTime(updatedAt)
	job.CompletedAt = parseTimePtr(completedAt)
	return &job, nil
}
