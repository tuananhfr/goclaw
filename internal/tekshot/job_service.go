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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	TekshotJobTypeDraftPosts = "draft_posts"
	TekshotJobTypePostChat   = "post_chat"
	TekshotJobTypeImageChat  = "image_chat"

	defaultJobPollInterval = 2 * time.Second
	defaultJobLockTTL      = 10 * time.Minute
	defaultJobRunTimeout   = 12 * time.Minute
)

type JobCreateRequest struct {
	ExternalJobUUID string         `json:"external_job_uuid"`
	ExternalUserID  string         `json:"external_user_id"`
	WorkspaceID     string         `json:"workspace_id"`
	WorkspaceUUID   string         `json:"workspace_uuid"`
	JobType         string         `json:"job_type"`
	AgentKey        string         `json:"agent_key"`
	SessionKey      string         `json:"session_key"`
	CallbackURL     string         `json:"callback_url"`
	CallbackToken   string         `json:"callback_token"`
	Request         map[string]any `json:"request"`
	ToolArgs        map[string]any `json:"tool_args"`
}

type JobService struct {
	store      store.TekshotJobStore
	agents     *agent.Router
	tools      *tools.Registry
	httpClient *http.Client
	workers    int
	wake       chan struct{}
}

func NewJobService(jobStore store.TekshotJobStore, agents *agent.Router, toolsReg *tools.Registry) *JobService {
	workers := envInt("GOCLAW_TEKSHOT_JOB_WORKERS", 1)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	return &JobService{
		store:  jobStore,
		agents: agents,
		tools:  toolsReg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		workers: workers,
		wake:    make(chan struct{}, 1),
	}
}

func (s *JobService) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	for i := 0; i < s.workers; i++ {
		workerID := i + 1
		go s.watch(ctx, workerID)
	}
	slog.Info("tekshot.jobs.started", "workers", s.workers)
}

func (s *JobService) Create(ctx context.Context, req JobCreateRequest) (*store.TekshotJob, error) {
	if strings.TrimSpace(req.ExternalJobUUID) == "" {
		return nil, fmt.Errorf("external_job_uuid is required")
	}
	if strings.TrimSpace(req.AgentKey) == "" {
		return nil, fmt.Errorf("agent_key is required")
	}
	req.JobType = strings.TrimSpace(req.JobType)
	if req.JobType == "" {
		req.JobType = TekshotJobTypeDraftPosts
	}
	if !isSupportedTekshotJobType(req.JobType) {
		return nil, fmt.Errorf("unsupported tekshot job type: %s", req.JobType)
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		req.SessionKey = "tekshot:" + req.JobType + ":" + uuid.NewString()
	}
	request := req.Request
	if request == nil {
		request = req.ToolArgs
	}
	if request == nil {
		request = map[string]any{}
	}
	request["agent_key"] = strings.TrimSpace(req.AgentKey)
	request["session_key"] = strings.TrimSpace(req.SessionKey)

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode job request: %w", err)
	}

	job, err := s.store.Create(ctx, &store.TekshotJob{
		ExternalJobUUID: strings.TrimSpace(req.ExternalJobUUID),
		WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
		WorkspaceUUID:   strings.TrimSpace(req.WorkspaceUUID),
		ExternalUserID:  strings.TrimSpace(req.ExternalUserID),
		JobType:         req.JobType,
		AgentKey:        strings.TrimSpace(req.AgentKey),
		SessionKey:      strings.TrimSpace(req.SessionKey),
		Status:          store.TekshotJobQueued,
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

func (s *JobService) Get(ctx context.Context, id uuid.UUID) (*store.TekshotJob, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("tekshot job service is not configured")
	}
	return s.store.Get(ctx, id)
}

func (s *JobService) watch(ctx context.Context, workerID int) {
	ticker := time.NewTicker(defaultJobPollInterval)
	defer ticker.Stop()

	for {
		if err := s.processNext(ctx, workerID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("tekshot.jobs.scan_failed", "worker", workerID, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *JobService) processNext(parent context.Context, workerID int) error {
	job, err := s.store.ClaimNext(parent, defaultJobLockTTL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if job == nil {
		return nil
	}

	slog.Info("tekshot.job.claimed", "worker", workerID, "job_id", job.ID, "job_type", job.JobType, "external_job_uuid", job.ExternalJobUUID)
	runCtx, cancel := context.WithTimeout(parent, defaultJobRunTimeout)
	defer cancel()

	if err := s.process(runCtx, job); err != nil {
		message := err.Error()
		_ = s.store.MarkFailed(context.Background(), job.ID, message)
		s.sendCallback(context.Background(), job, store.TekshotJobFailed, "Tekshot job failed", message, nil)
		slog.Error("tekshot.job.failed", "job_id", job.ID, "job_type", job.JobType, "error", err)
		return nil
	}
	return nil
}

func (s *JobService) process(ctx context.Context, job *store.TekshotJob) error {
	request := map[string]any{}
	if len(job.RequestJSON) > 0 {
		if err := json.Unmarshal(job.RequestJSON, &request); err != nil {
			return fmt.Errorf("decode job request: %w", err)
		}
	}

	_ = s.store.MarkRunning(ctx, job.ID, "Running in GoClaw", defaultJobLockTTL)
	s.sendCallback(ctx, job, store.TekshotJobRunning, "Running in GoClaw", "", nil)

	var result any
	var progress string
	var err error
	switch job.JobType {
	case TekshotJobTypeDraftPosts:
		result, progress, err = s.runDraftPosts(ctx, job, request)
	case TekshotJobTypePostChat, TekshotJobTypeImageChat:
		result, progress, err = s.runChat(ctx, job, request)
	default:
		err = fmt.Errorf("unsupported tekshot job type: %s", job.JobType)
	}
	if err != nil {
		return err
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode job result: %w", err)
	}
	if err := s.store.MarkCompleted(context.Background(), job.ID, encoded, progress); err != nil {
		return err
	}
	s.sendCallback(context.Background(), job, store.TekshotJobCompleted, progress, "", encoded)
	return nil
}

func (s *JobService) runDraftPosts(ctx context.Context, job *store.TekshotJob, args map[string]any) (any, string, error) {
	if s.tools == nil {
		return nil, "", fmt.Errorf("tools registry is not configured")
	}
	execCtx := store.WithTenantID(ctx, store.MasterTenantID)
	execCtx = store.WithUserID(execCtx, "tekshot-"+job.ExternalUserID)
	execCtx = store.WithAgentKey(execCtx, job.AgentKey)
	result := s.tools.Execute(execCtx, toolName, args)
	if result == nil {
		return nil, "", fmt.Errorf("draft generation returned no result")
	}
	if result.IsError {
		if result.ForLLM != "" {
			return nil, "", errors.New(result.ForLLM)
		}
		return nil, "", fmt.Errorf("draft generation failed")
	}

	structured := result.StructuredContent
	if structured == nil && result.ForLLM != "" {
		var decoded any
		if err := json.Unmarshal([]byte(result.ForLLM), &decoded); err == nil {
			structured = decoded
		}
	}
	if structured == nil {
		return nil, "", fmt.Errorf("draft generation returned no structured content")
	}
	return structured, "Draft posts generated", nil
}

func (s *JobService) runChat(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	message := stringFromMap(request, "prompt")
	if strings.TrimSpace(message) == "" {
		message = stringFromMap(request, "message")
	}
	if strings.TrimSpace(message) == "" {
		return nil, "", fmt.Errorf("chat message is required")
	}
	mediaFiles := mediaFromJobRequest(request)
	if len(mediaFiles) > 0 {
		mediaInfos := make([]media.MediaInfo, 0, len(mediaFiles))
		for _, item := range mediaFiles {
			mimeType := strings.TrimSpace(item.MimeType)
			if mimeType == "" {
				mimeType = media.DetectMIMEType(item.Path)
			}
			mediaInfos = append(mediaInfos, media.MediaInfo{
				Type:        media.MediaKindFromMime(mimeType),
				FilePath:    item.Path,
				ContentType: mimeType,
				FileName:    item.Filename,
			})
		}
		if tags := media.BuildMediaTags(mediaInfos); tags != "" {
			message = tags + "\n\n" + message
		}
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)
	runReq := agent.RunRequest{
		SessionKey:  job.SessionKey,
		Message:     message,
		Media:       mediaFiles,
		Channel:     "tekshot_job",
		ChannelType: "tekshot",
		ChatID:      userID,
		PeerKind:    "direct",
		Addressed:   true,
		RunID:       uuid.NewString(),
		UserID:      userID,
		SenderID:    userID,
		Stream:      false,
	}
	result, err := loop.Run(runCtx, runReq)
	if err != nil {
		return nil, "", err
	}
	if result == nil {
		return nil, "", fmt.Errorf("agent returned no result")
	}

	// image_chat must actually produce an image. Agents (especially with a
	// heavy image skill) sometimes narrate completion or stop at a text
	// "plan" without calling create_image. If the free first pass yielded no
	// delivered media, force a final create_image pass — mirrors the draft
	// flow's forced submit_draft_batch fallback (draft_posts_tool.go). All
	// other tools (skills, edit, references) still ran freely in pass one.
	if job.JobType == "image_chat" && len(result.Media) == 0 {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 2
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: "create_image"}
		finalReq.Message = "Create the final image NOW by calling create_image with a complete prompt. " +
			"All needed images are already attached — do NOT set reference_image_path. Do not reply with plain text."
		if forced, ferr := loop.Run(runCtx, finalReq); ferr == nil && forced != nil && len(forced.Media) > 0 {
			result = forced
		}
	}
	if job.JobType == "image_chat" && len(result.Media) == 0 {
		return nil, "", fmt.Errorf("tekshot image_chat: agent did not produce an image; please retry")
	}

	return map[string]any{
		"content": result.Content,
		"media":   result.Media,
		"usage":   result.Usage,
	}, "Completed", nil
}

func (s *JobService) sendCallback(ctx context.Context, job *store.TekshotJob, status, progress, errorMessage string, result json.RawMessage) {
	if strings.TrimSpace(job.CallbackURL) == "" {
		return
	}
	payload := map[string]any{
		"ok":                status != store.TekshotJobFailed,
		"goclaw_job_id":     job.ID.String(),
		"external_job_uuid": job.ExternalJobUUID,
		"job_type":          job.JobType,
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
		slog.Warn("tekshot.job.callback_request_failed", "job_id", job.ID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if job.CallbackToken != "" {
		req.Header.Set("Authorization", "Bearer "+job.CallbackToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Warn("tekshot.job.callback_failed", "job_id", job.ID, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("tekshot.job.callback_bad_status", "job_id", job.ID, "status", resp.StatusCode)
	}
}

func (s *JobService) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func isSupportedTekshotJobType(jobType string) bool {
	switch jobType {
	case TekshotJobTypeDraftPosts, TekshotJobTypePostChat, TekshotJobTypeImageChat:
		return true
	default:
		return false
	}
}

func stringFromMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func mediaFromJobRequest(request map[string]any) []bus.MediaFile {
	rawMedia, ok := request["media"].([]any)
	if !ok {
		return nil
	}
	media := make([]bus.MediaFile, 0, len(rawMedia))
	for _, item := range rawMedia {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := stringFromMap(record, "path")
		if strings.TrimSpace(path) == "" {
			continue
		}
		media = append(media, bus.MediaFile{
			Path:     path,
			MimeType: stringFromMap(record, "mime_type"),
			Filename: stringFromMap(record, "filename"),
		})
	}
	return media
}
