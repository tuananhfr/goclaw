package video

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/mediaworker"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

type JobStatus string

const (
	JobQueued          JobStatus = "queued"
	JobRunning         JobStatus = "running"
	JobRetryWait       JobStatus = "retry_wait"
	JobIngestingOutput JobStatus = "ingesting_output"
	JobSucceeded       JobStatus = "succeeded"
	JobFailed          JobStatus = "failed"
	JobCancelled       JobStatus = "cancelled"
)

type JobRequest struct {
	ContractVersion int                `json:"contract_version"`
	ExternalJobUUID string             `json:"external_job_uuid"`
	ExternalUserID  string             `json:"external_user_id"`
	Scope           JobScope           `json:"scope"`
	JobKind         string             `json:"job_kind"`
	ModelID         *string            `json:"model_id"`
	Operation       *string            `json:"operation"`
	Inputs          JobInputs          `json:"inputs"`
	Output          JobOutputRequest   `json:"output"`
	ProviderOptions map[string]any     `json:"provider_options"`
	Callback        JobCallback        `json:"callback"`
	Metadata        JobRequestMetadata `json:"metadata"`
}

type JobScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type JobInputs struct {
	Prompt          *string               `json:"prompt"`
	NegativePrompt  *string               `json:"negative_prompt"`
	ImageURL        *string               `json:"image_url"`
	FirstFrameURL   *string               `json:"first_frame_url"`
	LastFrameURL    *string               `json:"last_frame_url"`
	VideoURL        *string               `json:"video_url"`
	AudioURL        *string               `json:"audio_url"`
	ReferenceURLs   []string              `json:"reference_urls"`
	ReferenceInputs []ImageReferenceInput `json:"reference_inputs,omitempty"`
	ManifestURL     *string               `json:"manifest_url"`
	Manifest        *mediaworker.Manifest `json:"manifest,omitempty"`
}

// ImageReferenceInput preserves why a connected workflow image was supplied.
type ImageReferenceInput struct {
	URL         string `json:"url"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type JobOutputRequest struct {
	DurationMS  int    `json:"duration_ms"`
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	Count       int    `json:"count"`
	NativeAudio bool   `json:"native_audio"`
	Format      string `json:"format,omitempty"`
}

type JobCallback struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type JobRequestMetadata struct {
	ProjectUUID     string  `json:"project_uuid"`
	SceneUUID       *string `json:"scene_uuid"`
	ProjectRevision int     `json:"project_revision"`
	SceneRevision   *int    `json:"scene_revision"`
	RequestID       string  `json:"request_id"`
	IdempotencyKey  string  `json:"idempotency_key,omitempty"`
}

type JobOutput struct {
	VariantIndex   int     `json:"variant_index"`
	DownloadURL    string  `json:"download_url"`
	MIMEType       string  `json:"mime_type"`
	ChecksumSHA256 *string `json:"checksum_sha256"`
	FileSize       *int    `json:"file_size"`
	Width          *int    `json:"width,omitempty"`
	Height         *int    `json:"height,omitempty"`
	DurationMS     *int    `json:"duration_ms,omitempty"`
	Codec          *string `json:"codec,omitempty"`
}

type JobError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
}

type JobUsage struct {
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit"`
	Estimated bool    `json:"estimated"`
	CostMinor *int    `json:"cost_minor"`
	Currency  *string `json:"currency"`
}

type Job struct {
	ID                string            `json:"id"`
	ExternalJobUUID   string            `json:"external_job_uuid"`
	ProviderJobID     *string           `json:"provider_job_id"`
	Status            JobStatus         `json:"status"`
	Progress          int               `json:"progress"`
	Message           *string           `json:"message"`
	Outputs           []JobOutput       `json:"outputs"`
	Usage             *JobUsage         `json:"usage,omitempty"`
	Error             *JobError         `json:"error,omitempty"`
	CreatedAt         string            `json:"created_at"`
	ChangedAt         string            `json:"changed_at"`
	IdempotencyKey    string            `json:"-"`
	RequestHash       string            `json:"-"`
	Request           JobRequest        `json:"-"`
	Scenario          string            `json:"-"`
	OutputToken       string            `json:"-"`
	OutputBaseURL     string            `json:"-"`
	Sequence          int               `json:"-"`
	CallbackDelivered bool              `json:"-"`
	ProviderState     map[string]string `json:"-"`
}

type persistedJob struct {
	Job
	IdempotencyKey    string            `json:"idempotency_key"`
	RequestHash       string            `json:"request_hash"`
	Request           JobRequest        `json:"request"`
	Scenario          string            `json:"scenario"`
	OutputToken       string            `json:"output_token"`
	OutputBaseURL     string            `json:"output_base_url"`
	Sequence          int               `json:"sequence"`
	CallbackDelivered bool              `json:"callback_delivered"`
	ProviderState     map[string]string `json:"provider_state,omitempty"`
}

type CallbackEvent struct {
	ContractVersion int         `json:"contract_version"`
	EventID         string      `json:"event_id"`
	Sequence        int         `json:"sequence"`
	EventType       string      `json:"event_type"`
	ExternalJobUUID string      `json:"external_job_uuid"`
	GoClawJobID     string      `json:"goclaw_job_id"`
	ProviderJobID   *string     `json:"provider_job_id"`
	Status          JobStatus   `json:"status"`
	Progress        int         `json:"progress"`
	Message         *string     `json:"message"`
	Outputs         []JobOutput `json:"outputs"`
	Usage           *JobUsage   `json:"usage"`
	Error           *JobError   `json:"error"`
	OccurredAt      string      `json:"occurred_at"`
}

var errJobTerminal = errors.New("job is terminal")

type JobAPIError struct {
	Status int
	Code   string
	Err    error
}

func (e *JobAPIError) Error() string { return e.Err.Error() }
func (e *JobAPIError) Unwrap() error { return e.Err }

type FileJobStore struct {
	dir  string
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewFileJobStore(dir string) (*FileJobStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("video job store directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create video job store: %w", err)
	}
	store := &FileJobStore{dir: dir, jobs: make(map[string]Job)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read video job store: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("read video job %s: %w", entry.Name(), readErr)
		}
		var stored persistedJob
		if unmarshalErr := json.Unmarshal(data, &stored); unmarshalErr != nil {
			return nil, fmt.Errorf("decode video job %s: %w", entry.Name(), unmarshalErr)
		}
		job := stored.toJob()
		store.jobs[job.ID] = job
	}
	return store, nil
}

func (s *FileJobStore) Create(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return errors.New("video job already exists")
	}
	for _, existing := range s.jobs {
		if existing.IdempotencyKey == job.IdempotencyKey && existing.Request.ExternalUserID == job.Request.ExternalUserID {
			return errors.New("video job idempotency key already exists")
		}
	}
	if err := s.persistLocked(job); err != nil {
		return err
	}
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

func (s *FileJobStore) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return cloneJob(job), ok
}

func (s *FileJobStore) FindIdempotent(userID, key string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, job := range s.jobs {
		if job.Request.ExternalUserID == userID && job.IdempotencyKey == key {
			return cloneJob(job), true
		}
	}
	return Job{}, false
}

func (s *FileJobStore) Update(id string, mutate func(*Job) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, os.ErrNotExist
	}
	if err := mutate(&job); err != nil {
		return Job{}, err
	}
	if err := s.persistLocked(job); err != nil {
		return Job{}, err
	}
	s.jobs[id] = cloneJob(job)
	return cloneJob(job), nil
}

func (s *FileJobStore) Active() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, 0)
	for _, job := range s.jobs {
		if !isTerminal(job.Status) {
			jobs = append(jobs, cloneJob(job))
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt < jobs[j].CreatedAt })
	return jobs
}

func (s *FileJobStore) persistLocked(job Job) error {
	data, err := json.MarshalIndent(newPersistedJob(job), "", "  ")
	if err != nil {
		return fmt.Errorf("encode video job: %w", err)
	}
	temporary, err := os.CreateTemp(s.dir, ".video-job-*.tmp")
	if err != nil {
		return fmt.Errorf("create video job temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure video job temp file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write video job: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync video job: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close video job: %w", err)
	}
	if err := os.Rename(temporaryName, filepath.Join(s.dir, job.ID+".json")); err != nil {
		return fmt.Errorf("commit video job: %w", err)
	}
	return nil
}

type JobService struct {
	registry         ModelRegistry
	store            *FileJobStore
	httpClient       *http.Client
	delay            time.Duration
	mediaWorkerURL   string
	mediaWorkerToken string
	outputDir        string
	providers        map[string]RenderProvider
	cancelMu         sync.Mutex
	cancels          map[string]context.CancelFunc
}

// providerRenderTimeout bounds one provider job including queue wait and download.
const providerRenderTimeout = 30 * time.Minute

func NewJobService(registry ModelRegistry, store *FileJobStore, httpClient *http.Client, delay time.Duration, options ...JobServiceOption) *JobService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}
	service := &JobService{
		registry:         registry,
		store:            store,
		httpClient:       httpClient,
		delay:            delay,
		mediaWorkerURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_WORKER_URL")), "/"),
		mediaWorkerToken: strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_WORKER_TOKEN")),
		cancels:          make(map[string]context.CancelFunc),
	}
	for _, option := range options {
		option(service)
	}
	for _, job := range store.Active() {
		service.start(job.ID)
	}
	return service
}

func (s *JobService) Create(request JobRequest, idempotencyKey, outputBaseURL string) (Job, bool, error) {
	if err := s.validateRequest(request); err != nil {
		return Job{}, false, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return Job{}, false, &JobAPIError{Status: http.StatusUnprocessableEntity, Code: "INVALID_REQUEST", Err: err}
	}
	hash := sha256.Sum256(payload)
	requestHash := hex.EncodeToString(hash[:])
	if existing, ok := s.store.FindIdempotent(request.ExternalUserID, idempotencyKey); ok {
		if existing.RequestHash != requestHash {
			return Job{}, false, &JobAPIError{Status: http.StatusConflict, Code: "IDEMPOTENCY_CONFLICT", Err: errors.New("idempotency key was reused with a different request")}
		}
		return publicJob(existing), true, nil
	}
	provider := s.providerFor(request)
	scenario := stringOption(request.ProviderOptions, "scenario", "success")
	if provider != nil {
		// Drupal always sends its mock scenario; a real provider must not act it out.
		scenario = "success"
	}
	if scenario == "provider-unavailable" {
		return Job{}, false, &JobAPIError{Status: http.StatusServiceUnavailable, Code: "MODEL_UNAVAILABLE", Err: errors.New("mock provider is unavailable")}
	}
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 64 {
		return Job{}, false, &JobAPIError{Status: http.StatusBadRequest, Code: "INVALID_REQUEST", Err: errors.New("Idempotency-Key must contain 8 to 64 characters")}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var providerID *string
	if provider == nil {
		mockID := "mock-provider-job-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
		providerID = &mockID
	}
	job := Job{
		ID:              "goclaw-video-job-" + uuid.NewString(),
		ExternalJobUUID: request.ExternalJobUUID,
		ProviderJobID:   providerID,
		Status:          JobQueued,
		Progress:        0,
		Outputs:         []JobOutput{},
		CreatedAt:       now,
		ChangedAt:       now,
		IdempotencyKey:  idempotencyKey,
		RequestHash:     requestHash,
		Request:         request,
		Scenario:        scenario,
		OutputToken:     randomToken(),
		OutputBaseURL:   strings.TrimRight(outputBaseURL, "/"),
	}
	if err := s.store.Create(job); err != nil {
		return Job{}, false, err
	}
	s.start(job.ID)
	return publicJob(job), false, nil
}

func (s *JobService) Get(id string) (Job, bool) {
	job, ok := s.store.Get(id)
	return publicJob(job), ok
}

func (s *JobService) Cancel(id string) (Job, error) {
	job, err := s.store.Update(id, func(job *Job) error {
		if isTerminal(job.Status) {
			return nil
		}
		job.Status = JobCancelled
		job.Progress = min(job.Progress, 99)
		job.Error = nil
		message := "Mock provider cancellation confirmed."
		if s.providerFor(job.Request) != nil {
			message = "Đã huỷ job, đang báo nhà cung cấp dừng render."
		}
		job.Message = &message
		job.ChangedAt = time.Now().UTC().Format(time.RFC3339Nano)
		job.Sequence++
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	s.stopProvider(id)
	event := eventFromJob(job, "cancelled")
	s.deliver(job, event)
	if job.Scenario == "cancel-race-success" {
		go func() {
			defer safego.Recover(nil, "video_job_id", id)
			time.Sleep(s.delay)
			late := publicJob(job)
			late.Sequence++
			late.Status = JobSucceeded
			late.Progress = 100
			late.Outputs = s.outputsFor(job, false)
			message := "Late output arrived after cancellation."
			late.Message = &message
			s.deliver(job, eventFromJob(late, "completed"))
		}()
	}
	return publicJob(job), nil
}

func (s *JobService) Output(id string, variant int, token string) ([]byte, string, bool) {
	job, ok := s.store.Get(id)
	if !ok || token == "" || token != job.OutputToken || variant < 0 || variant >= max(1, len(job.Outputs)) {
		return nil, "", false
	}
	if s.providerFor(job.Request) != nil {
		data, err := os.ReadFile(s.outputPath(id, variant))
		if err != nil {
			return nil, "", false
		}
		return data, "video/mp4", true
	}
	return slices.Clone(mockVideoFixture), "video/mp4", true
}

func (s *JobService) start(id string) {
	go func() {
		defer safego.Recover(func(any) {
			if _, err := s.fail(id, "MOCK_PROVIDER_PANIC", "Mock provider worker stopped unexpectedly.", false); err != nil {
				slog.Error("video mock provider panic recovery failed", "job_id", id, "error", err)
			}
		}, "video_job_id", id)
		s.run(id)
	}()
}

func (s *JobService) run(id string) {
	job, ok := s.store.Get(id)
	if !ok || isTerminal(job.Status) {
		return
	}
	if job.Request.JobKind == "project.assemble" {
		s.runAssemble(id, job)
		return
	}
	if provider := s.providerFor(job.Request); provider != nil {
		s.runProvider(id, job, provider)
		return
	}
	if job.Sequence == 0 {
		job, _ = s.advance(id, JobQueued, 0, "Mock provider accepted the job.", "accepted", nil, nil)
	}
	time.Sleep(s.delay)
	if job.Scenario == "cancel-before-start" {
		_, _ = s.Cancel(id)
		return
	}
	if latest, exists := s.store.Get(id); !exists || isTerminal(latest.Status) {
		return
	}
	job, _ = s.advance(id, JobRunning, 10, "Mock provider started rendering.", "progress", nil, nil)

	switch job.Scenario {
	case "permanent-failure":
		time.Sleep(s.delay)
		_, _ = s.fail(id, "CONTENT_REJECTED", "Mock content policy rejected the request.", false)
		return
	case "timeout":
		time.Sleep(s.delay * 2)
		_, _ = s.fail(id, "PROVIDER_TIMEOUT", "Mock provider timed out.", true)
		return
	case "transient-failure":
		time.Sleep(s.delay)
		retry := 1
		err := &JobError{Code: "RATE_LIMITED", Message: "Mock provider rate limited the first attempt.", Retryable: true, RetryAfterSeconds: &retry}
		job, _ = s.advance(id, JobRetryWait, 20, "Waiting to retry the mock provider.", "error", nil, err)
		time.Sleep(s.delay)
		job, _ = s.advance(id, JobRunning, 35, "Mock provider retry started.", "progress", nil, nil)
	}

	progresses := []int{35, 55, 75}
	if job.Scenario == "slow" {
		progresses = []int{20, 35, 50, 65, 82}
	}
	if job.Scenario == "out-of-order-callback" {
		time.Sleep(s.delay)
		stored, err := s.store.Update(id, func(current *Job) error {
			if isTerminal(current.Status) {
				return errors.New("job is terminal")
			}
			current.Status = JobRunning
			current.Progress = 70
			current.Sequence += 2
			current.ChangedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return nil
		})
		if err == nil {
			s.deliver(stored, eventFromJob(stored, "progress"))
			stale := stored
			stale.Sequence--
			stale.Progress = 60
			s.deliver(stored, eventFromJob(stale, "progress"))
		}
	} else {
		for _, progress := range progresses {
			time.Sleep(s.delay)
			latest, exists := s.store.Get(id)
			if !exists || isTerminal(latest.Status) {
				return
			}
			job, _ = s.advance(id, JobRunning, progress, "Mock provider is rendering frames.", "progress", nil, nil)
		}
	}

	if job.Scenario == "invalid-output" {
		time.Sleep(s.delay)
		outputs := s.outputsFor(job, true)
		_, _ = s.advance(id, JobSucceeded, 100, "Mock provider returned an invalid output.", "completed", outputs, nil)
		return
	}
	time.Sleep(s.delay)
	latest, exists := s.store.Get(id)
	if !exists || isTerminal(latest.Status) {
		return
	}
	outputs := s.outputsFor(latest, false)
	completed, err := s.advance(id, JobSucceeded, 100, "Mock provider completed the job.", "completed", outputs, nil)
	if err != nil {
		return
	}
	if completed.Scenario == "callback-lost" {
		return
	}
}

func (s *JobService) providerFor(request JobRequest) RenderProvider {
	if request.JobKind != "scene.render" || request.ModelID == nil || len(s.providers) == 0 {
		return nil
	}
	model, ok := s.registry.Get(*request.ModelID)
	if !ok {
		return nil
	}
	return s.providers[model.Provider]
}

func (s *JobService) outputPath(id string, variant int) string {
	return filepath.Join(s.outputDir, filepath.Base(id), fmt.Sprintf("%d.mp4", variant))
}

func (s *JobService) stopProvider(id string) {
	s.cancelMu.Lock()
	cancel := s.cancels[id]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *JobService) runProvider(id string, job Job, provider RenderProvider) {
	model, ok := s.registry.Get(*job.Request.ModelID)
	if !ok {
		_, _ = s.fail(id, "MODEL_UNAVAILABLE", "Video model is no longer in the catalog.", false)
		return
	}
	if job.Sequence == 0 {
		job, _ = s.advance(id, JobQueued, 0, "Đang gửi job tới nhà cung cấp video.", "accepted", nil, nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerRenderTimeout)
	s.cancelMu.Lock()
	s.cancels[id] = cancel
	s.cancelMu.Unlock()
	defer func() {
		s.cancelMu.Lock()
		delete(s.cancels, id)
		s.cancelMu.Unlock()
		cancel()
	}()
	// Cancel may have landed between the queued event and registering cancel.
	if latest, exists := s.store.Get(id); !exists || isTerminal(latest.Status) {
		return
	}

	update := func(change RenderUpdate) {
		if change.ProviderState != nil {
			// Persist first: a restart without this state resubmits and bills twice.
			if _, err := s.store.Update(id, func(current *Job) error {
				if change.ProviderJobID != "" {
					providerJobID := change.ProviderJobID
					current.ProviderJobID = &providerJobID
				}
				current.ProviderState = change.ProviderState
				return nil
			}); err != nil {
				slog.Error("video provider state not persisted", "job_id", id, "error", err)
			}
		}
		if _, err := s.advance(id, JobRunning, change.Progress, change.Message, "progress", nil, nil); err != nil && !errors.Is(err, errJobTerminal) {
			slog.Warn("video provider progress not recorded", "job_id", id, "error", err)
		}
	}
	request := RenderRequest{Job: job, Model: model, ProviderState: job.ProviderState, OutputPath: s.outputPath(id, 0)}
	output, err := provider.Render(ctx, request, update)
	if latest, exists := s.store.Get(id); !exists || isTerminal(latest.Status) {
		return
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			_, _ = s.fail(id, "PROVIDER_TIMEOUT", "Nhà cung cấp video không trả kết quả kịp thời.", true)
			return
		}
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) {
			slog.Error("video provider render failed", "job_id", id, "error", err)
			providerErr = asProviderError(err)
		}
		_, _ = s.fail(id, providerErr.Code, providerErr.Message, providerErr.Retryable)
		return
	}
	latest, _ := s.store.Get(id)
	checksum := output.ChecksumSHA256
	fileSize := output.FileSize
	duration := latest.Request.Output.DurationMS
	result := JobOutput{
		VariantIndex:   0,
		DownloadURL:    outputDownloadPath(id, 0, latest.OutputToken),
		MIMEType:       output.MIMEType,
		ChecksumSHA256: &checksum,
		FileSize:       &fileSize,
		DurationMS:     &duration,
	}
	_, _ = s.advance(id, JobSucceeded, 100, "Đã render xong video.", "completed", []JobOutput{result}, nil)
}

func (s *JobService) runAssemble(id string, job Job) {
	if job.Sequence == 0 {
		job, _ = s.advance(id, JobQueued, 0, "Media worker accepted the immutable manifest.", "accepted", nil, nil)
	}
	if s.mediaWorkerURL == "" || len(s.mediaWorkerToken) < 32 {
		_, _ = s.fail(id, "MEDIA_WORKER_UNAVAILABLE", "Media worker is not configured.", true)
		return
	}
	latest, exists := s.store.Get(id)
	if !exists || isTerminal(latest.Status) {
		return
	}
	job, _ = s.advance(id, JobRunning, 15, "Media worker is normalizing scene media.", "progress", nil, nil)
	payload, err := json.Marshal(mediaworker.AssembleRequest{JobID: id, Manifest: *job.Request.Inputs.Manifest})
	if err != nil {
		_, _ = s.fail(id, "MEDIA_MANIFEST_INVALID", "Could not encode the assemble manifest.", false)
		return
	}
	request, err := http.NewRequest(http.MethodPost, s.mediaWorkerURL+"/v1/assemble", bytes.NewReader(payload))
	if err != nil {
		_, _ = s.fail(id, "MEDIA_WORKER_UNAVAILABLE", "Could not create the media worker request.", true)
		return
	}
	request.Header.Set("Authorization", "Bearer "+s.mediaWorkerToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		_, _ = s.fail(id, "MEDIA_WORKER_UNAVAILABLE", "Media worker request failed.", true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = fmt.Sprintf("Media worker returned HTTP %d.", response.StatusCode)
		}
		_, _ = s.fail(id, "MEDIA_ASSEMBLE_FAILED", message, response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500)
		return
	}
	var result mediaworker.AssembleResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.ContractVersion != mediaworker.ContractVersion || result.DownloadURL == "" || result.MIMEType != "video/mp4" || result.FileSize < 1 {
		_, _ = s.fail(id, "MEDIA_OUTPUT_INVALID", "Media worker returned an invalid output.", false)
		return
	}
	checksum := result.Checksum
	fileSize := int(result.FileSize)
	width, height, duration, codec := result.Width, result.Height, result.DurationMS, result.Codec
	output := JobOutput{
		VariantIndex:   0,
		DownloadURL:    result.DownloadURL,
		MIMEType:       result.MIMEType,
		ChecksumSHA256: &checksum,
		FileSize:       &fileSize,
		Width:          &width,
		Height:         &height,
		DurationMS:     &duration,
		Codec:          &codec,
	}
	_, _ = s.advance(id, JobSucceeded, 100, "Media worker assembled and verified the MP4 output.", "completed", []JobOutput{output}, nil)
}

func (s *JobService) advance(id string, status JobStatus, progress int, message, eventType string, outputs []JobOutput, jobError *JobError) (Job, error) {
	job, err := s.store.Update(id, func(job *Job) error {
		if isTerminal(job.Status) {
			return errJobTerminal
		}
		job.Status = status
		job.Progress = progress
		job.Message = &message
		job.Outputs = nonNilOutputs(outputs)
		job.Error = jobError
		job.Sequence++
		job.ChangedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	event := eventFromJob(job, eventType)
	if !(job.Scenario == "callback-lost" && status == JobSucceeded) {
		s.deliver(job, event)
	}
	if job.Scenario == "duplicate-callback" && status == JobSucceeded {
		s.deliver(job, event)
	}
	return job, nil
}

func (s *JobService) fail(id, code, message string, retryable bool) (Job, error) {
	return s.advance(id, JobFailed, 100, message, "error", nil, &JobError{Code: code, Message: message, Retryable: retryable})
}

func (s *JobService) outputsFor(job Job, invalid bool) []JobOutput {
	count := job.Request.Output.Count
	if job.Scenario == "multiple-outputs" {
		count = 4
	}
	count = max(1, min(count, 4))
	checksum := sha256.Sum256(mockVideoFixture)
	checksumString := hex.EncodeToString(checksum[:])
	if invalid {
		checksumString = strings.Repeat("0", 64)
	}
	fileSize := len(mockVideoFixture)
	width, height := dimensions(job.Request.Output.AspectRatio)
	duration, codec := job.Request.Output.DurationMS, "h264"
	outputs := make([]JobOutput, 0, count)
	for index := range count {
		copyChecksum := checksumString
		outputs = append(outputs, JobOutput{
			VariantIndex:   index,
			DownloadURL:    outputDownloadPath(job.ID, index, job.OutputToken),
			MIMEType:       "video/mp4",
			ChecksumSHA256: &copyChecksum,
			FileSize:       &fileSize,
			Width:          &width,
			Height:         &height,
			DurationMS:     &duration,
			Codec:          &codec,
		})
	}
	return outputs
}

func (s *JobService) deliver(job Job, event CallbackEvent) {
	if strings.TrimSpace(job.Request.Callback.URL) == "" || strings.TrimSpace(job.Request.Callback.Token) == "" {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("video callback encoding failed", "job_id", job.ID, "error", err)
		return
	}
	for attempt := 1; attempt <= 3; attempt++ {
		delivered, retryable := s.deliverOnce(job, event, payload)
		if delivered || !retryable {
			return
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
	}
	slog.Warn("video callback retries exhausted", "job_id", job.ID, "event_id", event.EventID)
}

func (s *JobService) deliverOnce(job Job, event CallbackEvent, payload []byte) (bool, bool) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, job.Request.Callback.URL, bytes.NewReader(payload))
	if err != nil {
		slog.Warn("video callback request invalid", "job_id", job.ID, "error", err)
		return false, false
	}
	request.Header.Set("Authorization", "Bearer "+job.Request.Callback.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		slog.Warn("video callback delivery failed", "job_id", job.ID, "event_id", event.EventID, "error", err)
		return false, true
	}
	_, copyErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if copyErr != nil || closeErr != nil {
		slog.Warn("video callback response read failed", "job_id", job.ID, "event_id", event.EventID)
		return false, true
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true, false
	}
	slog.Warn("video callback delivery rejected", "job_id", job.ID, "event_id", event.EventID, "status", response.StatusCode)
	return false, response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
}

func (s *JobService) validateRequest(request JobRequest) error {
	invalid := func(message string) error {
		return &JobAPIError{Status: http.StatusUnprocessableEntity, Code: "INVALID_REQUEST", Err: errors.New(message)}
	}
	if request.ContractVersion != ContractVersion || uuid.Validate(request.ExternalJobUUID) != nil || strings.TrimSpace(request.ExternalUserID) == "" {
		return invalid("invalid contract version, external job UUID, or external user ID")
	}
	if request.Scope.Type != "store" || uuid.Validate(request.Scope.ID) != nil || request.JobKind != "scene.render" && request.JobKind != "project.assemble" {
		return invalid("invalid job scope or kind")
	}
	if request.JobKind == "project.assemble" {
		if uuid.Validate(request.Metadata.ProjectUUID) != nil || request.Metadata.SceneUUID != nil || request.Metadata.ProjectRevision < 1 || request.Metadata.SceneRevision != nil || uuid.Validate(request.Metadata.RequestID) != nil {
			return invalid("invalid assemble job metadata")
		}
		if request.ModelID != nil || request.Operation != nil || request.Inputs.Manifest == nil {
			return invalid("assemble jobs require an inline manifest and no model or operation")
		}
		if err := mediaworker.ValidateManifest(*request.Inputs.Manifest); err != nil {
			return invalid("invalid assemble manifest: " + err.Error())
		}
		if request.Inputs.Manifest.ProjectUUID != request.Metadata.ProjectUUID || request.Inputs.Manifest.ProjectRevision != request.Metadata.ProjectRevision {
			return invalid("assemble manifest does not match job metadata")
		}
		callbackURL, callbackErr := url.Parse(request.Callback.URL)
		if callbackErr != nil || callbackURL.Host == "" || callbackURL.Scheme != "http" && callbackURL.Scheme != "https" || len(request.Callback.Token) < 43 {
			return invalid("callback URL and a 256-bit token are required")
		}
		return nil
	}
	if uuid.Validate(request.Metadata.ProjectUUID) != nil || request.Metadata.SceneUUID == nil || uuid.Validate(*request.Metadata.SceneUUID) != nil || request.Metadata.ProjectRevision < 1 || request.Metadata.SceneRevision == nil || *request.Metadata.SceneRevision < 1 || uuid.Validate(request.Metadata.RequestID) != nil {
		return invalid("invalid job metadata")
	}
	if request.ModelID == nil || request.Operation == nil {
		return invalid("model_id and operation are required")
	}
	model, ok := s.registry.Get(*request.ModelID)
	if !ok {
		return &JobAPIError{Status: http.StatusNotFound, Code: "MODEL_NOT_FOUND", Err: errors.New("video model not found")}
	}
	if model.Status == ModelDisabled || model.Status == ModelDeprecated {
		return &JobAPIError{Status: http.StatusUnprocessableEntity, Code: "MODEL_DISABLED", Err: errors.New("video model is not available for new jobs")}
	}
	if !slices.Contains(model.Operations, *request.Operation) {
		return &JobAPIError{Status: http.StatusUnprocessableEntity, Code: "UNSUPPORTED_OPERATION", Err: errors.New("operation is not supported by the model")}
	}
	if request.Output.Count < model.Capabilities.OutputCount.Min || request.Output.Count > model.Capabilities.OutputCount.Max {
		return invalid("output count exceeds model capability")
	}
	if !supportsDuration(model.Capabilities.Duration, request.Output.DurationMS) {
		return invalid("output duration exceeds model capability")
	}
	if !slices.Contains(model.Capabilities.AspectRatios, request.Output.AspectRatio) || !slices.Contains(model.Capabilities.Resolutions, request.Output.Resolution) {
		return invalid("output aspect ratio or resolution exceeds model capability")
	}
	if request.Output.NativeAudio && !model.Capabilities.NativeAudio {
		return invalid("native audio is not supported by the model")
	}
	prompt := strings.TrimSpace(stringValue(request.Inputs.Prompt))
	promptLength := utf8.RuneCountInString(prompt)
	if model.Capabilities.Prompt.Required && prompt == "" {
		return invalid("prompt is required by the model")
	}
	if prompt != "" && (promptLength < model.Capabilities.Prompt.MinLength || promptLength > model.Capabilities.Prompt.MaxLength) {
		return invalid("prompt length exceeds model capability")
	}
	if request.Inputs.NegativePrompt != nil && !model.Capabilities.Prompt.NegativePrompt {
		return invalid("negative prompt is not supported by the model")
	}
	if request.Inputs.ImageURL != nil && !model.Capabilities.Inputs.Image || request.Inputs.FirstFrameURL != nil && !model.Capabilities.Inputs.FirstFrame || request.Inputs.LastFrameURL != nil && !model.Capabilities.Inputs.LastFrame || request.Inputs.VideoURL != nil && !model.Capabilities.Inputs.Video || request.Inputs.AudioURL != nil && !model.Capabilities.Inputs.Audio {
		return invalid("one or more input types are not supported by the model")
	}
	referenceURLs, err := validateReferenceInputs(request.Inputs)
	if err != nil {
		return invalid(err.Error())
	}
	referenceCount := len(referenceURLs)
	referenceLimits := model.Capabilities.Inputs.ReferenceImages
	if referenceCount > 0 && (!referenceLimits.Supported || referenceCount < referenceLimits.Min || referenceCount > referenceLimits.Max) {
		return invalid("reference image count exceeds model capability")
	}
	callbackURL, callbackErr := url.Parse(request.Callback.URL)
	if callbackErr != nil || callbackURL.Host == "" || callbackURL.Scheme != "http" && callbackURL.Scheme != "https" || len(request.Callback.Token) < 43 {
		return invalid("callback URL and a 256-bit token are required")
	}
	return nil
}

func validateReferenceInputs(inputs JobInputs) ([]string, error) {
	urls := inputs.ReferenceURLs
	if len(inputs.ReferenceInputs) > 0 {
		urls = make([]string, 0, len(inputs.ReferenceInputs))
		for _, reference := range inputs.ReferenceInputs {
			if utf8.RuneCountInString(strings.TrimSpace(reference.Name)) > 255 || utf8.RuneCountInString(strings.TrimSpace(reference.Description)) > 2000 {
				return nil, errors.New("reference image name or description is too long")
			}
			urls = append(urls, reference.URL)
		}
		if len(inputs.ReferenceURLs) > 0 && !slices.Equal(inputs.ReferenceURLs, urls) {
			return nil, errors.New("reference_urls and reference_inputs do not match")
		}
	}
	for _, rawURL := range urls {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("reference image URL must be an absolute HTTP URL")
		}
	}
	return urls, nil
}

func supportsDuration(capability Duration, duration int) bool {
	if capability.Mode == "discrete" || capability.Mode == "allowed-values" {
		return slices.Contains(capability.ValuesMS, duration)
	}
	if duration < capability.MinMS || duration > capability.MaxMS {
		return false
	}
	return capability.StepMS <= 0 || (duration-capability.MinMS)%capability.StepMS == 0
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func eventFromJob(job Job, eventType string) CallbackEvent {
	return CallbackEvent{
		ContractVersion: ContractVersion,
		EventID:         uuid.NewString(),
		Sequence:        job.Sequence,
		EventType:       eventType,
		ExternalJobUUID: job.ExternalJobUUID,
		GoClawJobID:     job.ID,
		ProviderJobID:   job.ProviderJobID,
		Status:          job.Status,
		Progress:        job.Progress,
		Message:         job.Message,
		Outputs:         nonNilOutputs(job.Outputs),
		Usage:           job.Usage,
		Error:           job.Error,
		OccurredAt:      job.ChangedAt,
	}
}

// Relative on purpose: the caller resolves it against the GoClaw URL it already
// trusts, since a proxy in front of GoClaw hides the public scheme and host.
func outputDownloadPath(id string, variant int, token string) string {
	return fmt.Sprintf("/v1/video/jobs/%s/outputs/%d?token=%s", url.PathEscape(id), variant, url.QueryEscape(token))
}

// Drupal rejects a callback whose outputs is not a JSON array, so nil must encode as [].
func nonNilOutputs(outputs []JobOutput) []JobOutput {
	if outputs == nil {
		return []JobOutput{}
	}
	return slices.Clone(outputs)
}

func publicJob(job Job) Job {
	job.IdempotencyKey = ""
	job.RequestHash = ""
	job.Request = JobRequest{}
	job.Scenario = ""
	job.OutputToken = ""
	job.OutputBaseURL = ""
	job.Sequence = 0
	job.CallbackDelivered = false
	job.ProviderState = nil
	return cloneJob(job)
}

func cloneJob(job Job) Job {
	data, err := json.Marshal(newPersistedJob(job))
	if err != nil {
		panic(err)
	}
	var cloned persistedJob
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(err)
	}
	return cloned.toJob()
}

func newPersistedJob(job Job) persistedJob {
	return persistedJob{
		Job:               job,
		IdempotencyKey:    job.IdempotencyKey,
		RequestHash:       job.RequestHash,
		Request:           job.Request,
		Scenario:          job.Scenario,
		OutputToken:       job.OutputToken,
		OutputBaseURL:     job.OutputBaseURL,
		Sequence:          job.Sequence,
		CallbackDelivered: job.CallbackDelivered,
		ProviderState:     job.ProviderState,
	}
}

func (stored persistedJob) toJob() Job {
	job := stored.Job
	job.IdempotencyKey = stored.IdempotencyKey
	job.RequestHash = stored.RequestHash
	job.Request = stored.Request
	job.Scenario = stored.Scenario
	job.OutputToken = stored.OutputToken
	job.OutputBaseURL = stored.OutputBaseURL
	job.Sequence = stored.Sequence
	job.CallbackDelivered = stored.CallbackDelivered
	job.ProviderState = stored.ProviderState
	return job
}

func isTerminal(status JobStatus) bool {
	return status == JobSucceeded || status == JobFailed || status == JobCancelled
}

func stringOption(options map[string]any, key, fallback string) string {
	value, ok := options[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func randomToken() string {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func dimensions(aspectRatio string) (int, int) {
	if aspectRatio == "9:16" {
		return 720, 1280
	}
	return 1280, 720
}

// A deterministic, MP4-signature fixture. Phase 5 replaces this mock payload
// with worker-generated media and ffprobe validation.
var mockVideoFixture = []byte{
	0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
	0x00, 0x00, 0x02, 0x00, 'i', 's', 'o', 'm', 'i', 's', 'o', '2',
	0x00, 0x00, 0x00, 0x08, 'm', 'd', 'a', 't',
}
