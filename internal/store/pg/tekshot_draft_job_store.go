package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTekshotDraftJobStore implements Tekshot draft jobs on Postgres.
type PGTekshotDraftJobStore struct {
	db *sql.DB
}

func NewPGTekshotDraftJobStore(db *sql.DB) *PGTekshotDraftJobStore {
	return &PGTekshotDraftJobStore{db: db}
}

func (s *PGTekshotDraftJobStore) Create(ctx context.Context, job *store.TekshotDraftJob) (*store.TekshotDraftJob, error) {
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

	_, err := s.db.ExecContext(ctx, `INSERT INTO tekshot_draft_jobs
		(id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		 status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		 attempt_count, locked_until, created_at, updated_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (external_job_uuid) DO NOTHING`,
		job.ID, job.ExternalJobUUID, job.WorkspaceID, job.WorkspaceUUID, job.ExternalUserID, job.AgentKey, job.SessionKey,
		job.Status, job.ProgressMessage, job.ErrorMessage, []byte(job.RequestJSON), []byte(job.ResultJSON), job.CallbackURL, job.CallbackToken,
		job.AttemptCount, job.LockedUntil, job.CreatedAt, job.UpdatedAt, job.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	return s.GetByExternalJobUUID(ctx, job.ExternalJobUUID)
}

func (s *PGTekshotDraftJobStore) Get(ctx context.Context, id uuid.UUID) (*store.TekshotDraftJob, error) {
	return s.scanOne(ctx, `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		attempt_count, locked_until, created_at, updated_at, completed_at
		FROM tekshot_draft_jobs WHERE id = $1`, id)
}

func (s *PGTekshotDraftJobStore) GetByExternalJobUUID(ctx context.Context, externalJobUUID string) (*store.TekshotDraftJob, error) {
	return s.scanOne(ctx, `SELECT id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
		status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
		attempt_count, locked_until, created_at, updated_at, completed_at
		FROM tekshot_draft_jobs WHERE external_job_uuid = $1`, externalJobUUID)
}

func (s *PGTekshotDraftJobStore) ClaimNext(ctx context.Context, lockFor time.Duration) (*store.TekshotDraftJob, error) {
	now := time.Now().UTC()
	lockedUntil := now.Add(lockFor)
	row := s.db.QueryRowContext(ctx, `UPDATE tekshot_draft_jobs
		SET status = $1, progress_message = $2, attempt_count = attempt_count + 1, locked_until = $3, updated_at = $4
		WHERE id = (
			SELECT id FROM tekshot_draft_jobs
			WHERE status = $5 AND (locked_until IS NULL OR locked_until <= $6)
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, external_job_uuid, workspace_id, workspace_uuid, external_user_id, agent_key, session_key,
			status, progress_message, error_message, request_json, result_json, callback_url, callback_token,
			attempt_count, locked_until, created_at, updated_at, completed_at`,
		store.TekshotDraftJobRunning, "Starting draft generation", lockedUntil, now, store.TekshotDraftJobQueued, now)
	return scanTekshotDraftJob(row)
}

func (s *PGTekshotDraftJobStore) MarkRunning(ctx context.Context, id uuid.UUID, progress string, lockFor time.Duration) error {
	if progress == "" {
		progress = "Generating draft posts"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = $1, progress_message = $2, locked_until = $3, updated_at = $4 WHERE id = $5`,
		store.TekshotDraftJobRunning, progress, time.Now().UTC().Add(lockFor), time.Now().UTC(), id)
	return err
}

func (s *PGTekshotDraftJobStore) MarkCompleted(ctx context.Context, id uuid.UUID, result json.RawMessage, progress string) error {
	if progress == "" {
		progress = "Draft posts generated"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = $1, progress_message = $2, error_message = '', result_json = $3, locked_until = NULL, completed_at = $4, updated_at = $5 WHERE id = $6`,
		store.TekshotDraftJobCompleted, progress, []byte(result), now, now, id)
	return err
}

func (s *PGTekshotDraftJobStore) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tekshot_draft_jobs SET status = $1, progress_message = $2, error_message = $3, locked_until = NULL, completed_at = $4, updated_at = $5 WHERE id = $6`,
		store.TekshotDraftJobFailed, "Draft generation failed", message, now, now, id)
	return err
}

func (s *PGTekshotDraftJobStore) scanOne(ctx context.Context, query string, args ...any) (*store.TekshotDraftJob, error) {
	job, err := scanTekshotDraftJob(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func scanTekshotDraftJob(row interface {
	Scan(dest ...any) error
}) (*store.TekshotDraftJob, error) {
	var job store.TekshotDraftJob
	var requestJSON, resultJSON []byte
	if err := row.Scan(
		&job.ID, &job.ExternalJobUUID, &job.WorkspaceID, &job.WorkspaceUUID, &job.ExternalUserID, &job.AgentKey, &job.SessionKey,
		&job.Status, &job.ProgressMessage, &job.ErrorMessage, &requestJSON, &resultJSON, &job.CallbackURL, &job.CallbackToken,
		&job.AttemptCount, &job.LockedUntil, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	); err != nil {
		return nil, err
	}
	job.RequestJSON = json.RawMessage(requestJSON)
	job.ResultJSON = json.RawMessage(resultJSON)
	return &job, nil
}
