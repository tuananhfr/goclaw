package tekshot

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultDraftJobPollInterval = 2 * time.Second
	defaultDraftJobLockTTL      = 10 * time.Minute
	defaultDraftJobRunTimeout   = 12 * time.Minute
)

// DraftJobCreateRequest is the public async Tekshot job creation payload.
type DraftJobCreateRequest struct {
	ExternalJobUUID string         `json:"external_job_uuid"`
	ExternalUserID  string         `json:"external_user_id"`
	WorkspaceID     string         `json:"workspace_id"`
	WorkspaceUUID   string         `json:"workspace_uuid"`
	AgentKey        string         `json:"agent_key"`
	SessionKey      string         `json:"session_key"`
	CallbackURL     string         `json:"callback_url"`
	CallbackToken   string         `json:"callback_token"`
	ToolArgs        map[string]any `json:"tool_args"`
}

// DraftJobService owns durable Tekshot draft generation jobs inside GoClaw.
type DraftJobService struct {
	store      store.TekshotDraftJobStore
	tools      *tools.Registry
	httpClient *http.Client
	workers    int
	wake       chan struct{}
}

func NewDraftJobService(jobStore store.TekshotDraftJobStore, toolsReg *tools.Registry) *DraftJobService {
	workers := max(envInt("GOCLAW_TEKSHOT_DRAFT_WORKERS", 1), 1)
	if workers > 8 {
		workers = 8
	}
	return &DraftJobService{
		store: jobStore,
		tools: toolsReg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		workers: workers,
		wake:    make(chan struct{}, 1),
	}
}

func (s *DraftJobService) Start(ctx context.Context) {
	if s == nil || s.store == nil || s.tools == nil {
		return
	}
	for i := 0; i < s.workers; i++ {
		workerID := i + 1
		go s.watch(ctx, workerID)
	}
	slog.Info("tekshot.draft_jobs.started", "workers", s.workers)
}

func (s *DraftJobService) Create(ctx context.Context, req DraftJobCreateRequest) (*store.TekshotDraftJob, error) {
	if strings.TrimSpace(req.ExternalJobUUID) == "" {
		return nil, fmt.Errorf("external_job_uuid is required")
	}
	if strings.TrimSpace(req.AgentKey) == "" {
		return nil, fmt.Errorf("agent_key is required")
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		req.SessionKey = "tekshot:draft:" + uuid.NewString()
	}
	if req.ToolArgs == nil {
		req.ToolArgs = map[string]any{}
	}
	req.ToolArgs["agent_key"] = strings.TrimSpace(req.AgentKey)
	req.ToolArgs["session_key"] = strings.TrimSpace(req.SessionKey)

	requestJSON, err := json.Marshal(req.ToolArgs)
	if err != nil {
		return nil, fmt.Errorf("encode tool args: %w", err)
	}

	job, err := s.store.Create(ctx, &store.TekshotDraftJob{
		ExternalJobUUID: strings.TrimSpace(req.ExternalJobUUID),
		WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
		WorkspaceUUID:   strings.TrimSpace(req.WorkspaceUUID),
		ExternalUserID:  strings.TrimSpace(req.ExternalUserID),
		AgentKey:        strings.TrimSpace(req.AgentKey),
		SessionKey:      strings.TrimSpace(req.SessionKey),
		Status:          store.TekshotDraftJobQueued,
		ProgressMessage: "Queued",
		RequestJSON:     requestJSON,
		CallbackURL:     strings.TrimSpace(req.CallbackURL),
		CallbackToken:   strings.TrimSpace(req.CallbackToken),
	})
	if err != nil {
		return nil, err
	}
	s.notify()
	return job, nil
}

func (s *DraftJobService) Get(ctx context.Context, id uuid.UUID) (*store.TekshotDraftJob, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("draft job service is not configured")
	}
	return s.store.Get(ctx, id)
}

func (s *DraftJobService) watch(ctx context.Context, workerID int) {
	ticker := time.NewTicker(defaultDraftJobPollInterval)
	defer ticker.Stop()

	for {
		if err := s.processNext(ctx, workerID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("tekshot.draft_jobs.scan_failed", "worker", workerID, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *DraftJobService) processNext(parent context.Context, workerID int) error {
	job, err := s.store.ClaimNext(parent, defaultDraftJobLockTTL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if job == nil {
		return nil
	}

	slog.Info("tekshot.draft_job.claimed", "worker", workerID, "job_id", job.ID, "external_job_uuid", job.ExternalJobUUID)
	runCtx, cancel := context.WithTimeout(parent, defaultDraftJobRunTimeout)
	defer cancel()

	if err := s.process(runCtx, job); err != nil {
		message := err.Error()
		_ = s.store.MarkFailed(context.Background(), job.ID, message)
		s.sendCallback(context.Background(), job, store.TekshotDraftJobFailed, "Draft generation failed", message, nil)
		slog.Error("tekshot.draft_job.failed", "job_id", job.ID, "error", err)
		return nil
	}
	return nil
}

func (s *DraftJobService) process(ctx context.Context, job *store.TekshotDraftJob) error {
	args := map[string]any{}
	if len(job.RequestJSON) > 0 {
		if err := json.Unmarshal(job.RequestJSON, &args); err != nil {
			return fmt.Errorf("decode job request: %w", err)
		}
	}
	_ = s.store.MarkRunning(ctx, job.ID, "Streaming from GoClaw", defaultDraftJobLockTTL)
	s.sendCallback(ctx, job, store.TekshotDraftJobRunning, "Streaming from GoClaw", "", nil)

	execCtx := store.WithTenantID(ctx, store.MasterTenantID)
	execCtx = store.WithUserID(execCtx, "tekshot-"+job.ExternalUserID)
	execCtx = store.WithAgentKey(execCtx, job.AgentKey)
	result := s.tools.Execute(execCtx, toolName, args)
	if result == nil {
		return fmt.Errorf("draft generation returned no result")
	}
	if result.IsError {
		if result.ForLLM != "" {
			return errors.New(result.ForLLM)
		}
		return fmt.Errorf("draft generation failed")
	}

	var structured any = result.StructuredContent
	if structured == nil && result.ForLLM != "" {
		var decoded any
		if err := json.Unmarshal([]byte(result.ForLLM), &decoded); err == nil {
			structured = decoded
		}
	}
	if structured == nil {
		return fmt.Errorf("draft generation returned no structured content")
	}

	encoded, err := json.Marshal(structured)
	if err != nil {
		return fmt.Errorf("encode draft result: %w", err)
	}
	if err := s.store.MarkCompleted(context.Background(), job.ID, encoded, "Draft posts generated"); err != nil {
		return err
	}
	s.sendCallback(context.Background(), job, store.TekshotDraftJobCompleted, "Draft posts generated", "", encoded)
	return nil
}

func (s *DraftJobService) sendCallback(ctx context.Context, job *store.TekshotDraftJob, status, progress, errorMessage string, result json.RawMessage) {
	if strings.TrimSpace(job.CallbackURL) == "" {
		return
	}
	payload := map[string]any{
		"ok":                status != store.TekshotDraftJobFailed,
		"goclaw_job_id":     job.ID.String(),
		"external_job_uuid": job.ExternalJobUUID,
		"status":            status,
		"progress_message":  progress,
		"error_message":     errorMessage,
		"session_key":       job.SessionKey,
	}
	if len(result) > 0 {
		var decoded any
		if err := json.Unmarshal(result, &decoded); err == nil {
			payload["result"] = decoded
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.CallbackURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("tekshot.draft_job.callback_request_failed", "job_id", job.ID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if job.CallbackToken != "" {
		req.Header.Set("Authorization", "Bearer "+job.CallbackToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Warn("tekshot.draft_job.callback_failed", "job_id", job.ID, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("tekshot.draft_job.callback_bad_status", "job_id", job.ID, "status", resp.StatusCode)
	}
}

func (s *DraftJobService) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
