package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	TekshotJobQueued    = "queued"
	TekshotJobRunning   = "running"
	TekshotJobCompleted = "completed"
	TekshotJobFailed    = "failed"
)

type TekshotJob struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	ExternalJobUUID string          `json:"external_job_uuid" db:"external_job_uuid"`
	WorkspaceID     string          `json:"workspace_id" db:"workspace_id"`
	WorkspaceUUID   string          `json:"workspace_uuid" db:"workspace_uuid"`
	ExternalUserID  string          `json:"external_user_id" db:"external_user_id"`
	JobType         string          `json:"job_type" db:"job_type"`
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

type TekshotJobStore interface {
	Create(ctx context.Context, job *TekshotJob) (*TekshotJob, error)
	Get(ctx context.Context, id uuid.UUID) (*TekshotJob, error)
	GetByExternalJobUUID(ctx context.Context, externalJobUUID string) (*TekshotJob, error)
	ClaimNext(ctx context.Context, lockFor time.Duration) (*TekshotJob, error)
	MarkRunning(ctx context.Context, id uuid.UUID, progress string, lockFor time.Duration) error
	MarkCompleted(ctx context.Context, id uuid.UUID, result json.RawMessage, progress string) error
	MarkFailed(ctx context.Context, id uuid.UUID, message string) error
}
