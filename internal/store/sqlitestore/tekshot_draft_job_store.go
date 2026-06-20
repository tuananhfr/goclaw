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

// SQLiteTekshotDraftJobStore implements Tekshot draft jobs on SQLite.
type SQLiteTekshotDraftJobStore struct {
	db *sql.DB
}

func NewSQLiteTekshotDraftJobStore(db *sql.DB) *SQLiteTekshotDraftJobStore {
	return &SQLiteTekshotDraftJobStore{db: db}
}

func (s *SQLiteTekshotDraftJobStore) Create(ctx context.Context, job *store.TekshotDraftJob) (*store.TekshotDraftJob, error) {
	if existing, err := s.GetByExternalJobUUID(ctx, job.ExternalJobUUID); err == nil && existing != nil {
		return existing, nil
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now().UTC()
	if job.Status == "" {
		job.Status = store.TekshotDraftJobQueued
	}
	if job.ProgressMessage == "" {
		job.ProgressMessage = "Queued"
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tekshot_draft_jobs
		(id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		 status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		 attempt_count, locked_until, created_at, updated_at, completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID.String(), job.ExternalJobUUID, job.WorkspaceID, job.WorkspaceUUID, job.ExternalUserID, job.AgentKey, job.SessionKey,
		job.Status, job.ProgressMessage, job.ErrorMessage, string(job.RequestJSON), string(job.ResultJSON), job.CallbackURL, job.CallbackToken,
		job.AttemptCount, timePtrToString(job.LockedUntil), job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano), timePtrToString(job.CompletedAt),
	)
	if err != nil {
		return nil, err
	}
	return s.GetByExternalJobUUID(ctx, job.ExternalJobUUID)
}

func (s *SQLiteTekshotDraftJobStore) Get(ctx context.Context, id uuid.UUID) (*store.TekshotDraftJob, error) {
	return s.scanOne(ctx, `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		attempt_count, locked_until, created_at, updated_at, completed_at
		FROM tekshot_draft_jobs WHERE id = ?`, id.String())
}

func (s *SQLiteTekshotDraftJobStore) GetByExternalJobUUID(ctx context.Context, externalJobUUID string) (*store.TekshotDraftJob, error) {
	return s.scanOne(ctx, `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		attempt_count, locked_until, created_at, updated_at, completed_at
		FROM tekshot_draft_jobs WHERE external_job_uuid = ?`, externalJobUUID)
}

func (s *SQLiteTekshotDraftJobStore) ClaimNext(ctx context.Context, lockFor time.Duration) (*store.TekshotDraftJob, error) {
	now := time.Now().UTC()
	lockedUntil := now.Add(lockFor)
	res, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs
		SET status = ?, progress_message = ?, attempt_count = attempt_count + 1, locked_until = ?, updated_at = ?
		WHERE id = (
			SELECT id FROM tekshot_draft_jobs
			WHERE status = ? AND (locked_until = '' OR locked_until <= ?)
			ORDER BY created_at ASC
			LIMIT 1
		)`,
		store.TekshotDraftJobRunning, "Starting draft generation", lockedUntil.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), store.TekshotDraftJobQueued, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	return s.scanOne(ctx, `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		attempt_count, locked_until, created_at, updated_at, completed_at
		FROM tekshot_draft_jobs WHERE status = ? AND locked_until = ? ORDER BY updated_at DESC LIMIT 1`,
		store.TekshotDraftJobRunning, lockedUntil.Format(time.RFC3339Nano))
}

func (s *SQLiteTekshotDraftJobStore) MarkRunning(ctx context.Context, id uuid.UUID, progress string, lockFor time.Duration) error {
	if progress == "" {
		progress = "Generating draft posts"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = ?, progress_message = ?, locked_until = ?, updated_at = ? WHERE id = ?`,
		store.TekshotDraftJobRunning, progress, now.Add(lockFor).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

func (s *SQLiteTekshotDraftJobStore) MarkCompleted(ctx context.Context, id uuid.UUID, result json.RawMessage, progress string) error {
	if progress == "" {
		progress = "Draft posts generated"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = ?, progress_message = ?, error_message = '', result_json = ?, locked_until = '', completed_at = ?, updated_at = ? WHERE id = ?`,
		store.TekshotDraftJobCompleted, progress, string(result), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

func (s *SQLiteTekshotDraftJobStore) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = ?, progress_message = ?, error_message = ?, locked_until = '', completed_at = ?, updated_at = ? WHERE id = ?`,
		store.TekshotDraftJobFailed, "Draft generation failed", message, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String())
	return err
}

func (s *SQLiteTekshotDraftJobStore) scanOne(ctx context.Context, query string, args ...any) (*store.TekshotDraftJob, error) {
	job, err := scanSQLiteTekshotDraftJob(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func scanSQLiteTekshotDraftJob(row interface {
	Scan(dest ...any) error
}) (*store.TekshotDraftJob, error) {
	var job store.TekshotDraftJob
	var id, lockedUntil, createdAt, updatedAt, completedAt string
	var requestJSON, resultJSON string
	if err := row.Scan(
		&id, &job.ExternalJobUUID, &job.WorkspaceID, &job.WorkspaceUUID, &job.ExternalUserID, &job.AgentKey, &job.SessionKey,
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

func timePtrToString(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimePtr(value string) *time.Time {
	if value == "" {
		return nil
	}
	t := parseTime(value)
	if t.IsZero() {
		return nil
	}
	return &t
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
