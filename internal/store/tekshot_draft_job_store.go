package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	TekshotDraftJobQueued    = "queued"
	TekshotDraftJobRunning   = "running"
	TekshotDraftJobCompleted = "completed"
	TekshotDraftJobFailed    = "failed"
)

// TekshotDraftJob is a durable async draft generation job owned by GoClaw.
type TekshotDraftJob struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	ExternalJobUUID string          `json:"external_job_uuid" db:"external_job_uuid"`
	WorkspaceID     string          `json:"workspace_id" db:"workspace_id"`
	WorkspaceUUID   string          `json:"workspace_uuid" db:"workspace_uuid"`
	ExternalUserID  string          `json:"external_user_id" db:"external_user_id"`
	AgentKey        string          `json:"agent_key" db:"agent_key"`
	SessionKey      string          `json:"session_key" db:"session_key"`
	Status          string          `json:"status" db:"status"`
	ProgressMessage string          `json:"progress_message" db:"progress_message"`
	ErrorMessage    string          `json:"error_message" db:"error_message"`
	RequestJSON     json.RawMessage `json:"request_json" db:"request_json"`
	ResultJSON      json.RawMessage `json:"result_json" db:"result_json"`
	CallbackURL     string          `json:"callback_url" db:"callback_url"`
	CallbackToken   string          `json:"-" db:"callback_token"`
	AttemptCount    int             `json:"attempt_count" db:"attempt_count"`
	LockedUntil     *time.Time      `json:"locked_until" db:"locked_until"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
	CompletedAt     *time.Time      `json:"completed_at" db:"completed_at"`
}

// TekshotDraftJobStore persists GoClaw-owned Tekshot draft jobs.
type TekshotDraftJobStore interface {
	Create(ctx context.Context, job *TekshotDraftJob) (*TekshotDraftJob, error)
	Get(ctx context.Context, id uuid.UUID) (*TekshotDraftJob, error)
	GetByExternalJobUUID(ctx context.Context, externalJobUUID string) (*TekshotDraftJob, error)
	ClaimNext(ctx context.Context, lockFor time.Duration) (*TekshotDraftJob, error)
	MarkRunning(ctx context.Context, id uuid.UUID, progress string, lockFor time.Duration) error
	MarkCompleted(ctx context.Context, id uuid.UUID, result json.RawMessage, progress string) error
	MarkFailed(ctx context.Context, id uuid.UUID, message string) error
}
