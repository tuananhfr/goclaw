package video

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var fakeMP4 = append([]byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, []byte("rendered-frames")...)

type fakeFal struct {
	t             *testing.T
	queue         *httptest.Server
	media         *httptest.Server
	submits       atomic.Int32
	cancels       atomic.Int32
	statusCalls   atomic.Int32
	mu            sync.Mutex
	submitted     map[string]any
	submitPath    string
	authHeader    string
	submitStatus  int
	submitBody    string
	statusURLHost string
	statuses      []string
	videoBody     []byte
	blockStatus   bool
}

func newFakeFal(t *testing.T) *fakeFal {
	t.Helper()
	fake := &fakeFal{t: t, submitStatus: http.StatusOK, statuses: []string{falStatusInQueue, falStatusInProgress, falStatusCompleted}, videoBody: fakeMP4}
	fake.media = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/scene.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		case "/voice.wav":
			// Drupal serves VieNeu narration with this non-canonical type.
			w.Header().Set("Content-Type", "audio/x-wav")
			_, _ = w.Write([]byte("wav-bytes"))
		case "/output.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fake.videoBody)
		default:
			http.NotFound(w, r)
		}
	}))
	fake.queue = httptest.NewServer(http.HandlerFunc(fake.serveQueue))
	t.Cleanup(func() {
		fake.queue.Close()
		fake.media.Close()
	})
	return fake
}

func (f *fakeFal) serveQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/fal-ai/"):
		f.submits.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.submitPath = r.URL.Path
		f.authHeader = r.Header.Get("Authorization")
		_ = json.Unmarshal(body, &f.submitted)
		f.mu.Unlock()
		if f.submitStatus != http.StatusOK {
			w.WriteHeader(f.submitStatus)
			_, _ = w.Write([]byte(f.submitBody))
			return
		}
		statusHost := f.queue.URL
		if f.statusURLHost != "" {
			statusHost = f.statusURLHost
		}
		// fal answers under the app root, not the full endpoint path.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id":   "req-1",
			"status":       falStatusInQueue,
			"status_url":   statusHost + "/fal-ai/wan/requests/req-1/status",
			"response_url": f.queue.URL + "/fal-ai/wan/requests/req-1",
			"cancel_url":   f.queue.URL + "/fal-ai/wan/requests/req-1/cancel",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/fal-ai/wan/requests/req-1/status":
		call := int(f.statusCalls.Add(1)) - 1
		if f.blockStatus {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": falStatusInProgress})
			return
		}
		status := f.statuses[min(call, len(f.statuses)-1)]
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status})
	case r.Method == http.MethodGet && r.URL.Path == "/fal-ai/wan/requests/req-1":
		_ = json.NewEncoder(w).Encode(map[string]any{"video": map[string]any{"url": f.media.URL + "/output.mp4", "content_type": "video/mp4"}, "duration": 7.25})
	case r.Method == http.MethodPut && r.URL.Path == "/fal-ai/wan/requests/req-1/cancel":
		f.cancels.Add(1)
		w.WriteHeader(http.StatusAccepted)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeFal) provider() *FalProvider {
	return &FalProvider{
		apiKey:        "test-key",
		queueURL:      f.queue.URL,
		apiClient:     f.queue.Client(),
		mediaClient:   f.media.Client(),
		pollInterval:  time.Millisecond,
		skipSSRFCheck: true,
	}
}

func falJobRequest(callbackURL, imageURL string) JobRequest {
	request := validJobRequest(callbackURL, "permanent-failure")
	modelID, operation := "fal/wan-2.2-a14b-i2v", "image-to-video"
	request.ModelID = &modelID
	request.Operation = &operation
	request.Inputs.ImageURL = &imageURL
	request.Output = JobOutputRequest{DurationMS: 5000, AspectRatio: "9:16", Resolution: "480p", Count: 1, Format: "mp4"}
	return request
}

func falModel(t *testing.T) Model {
	t.Helper()
	return falModelByID(t, "fal/wan-2.2-a14b-i2v")
}

func falModelByID(t *testing.T, id string) Model {
	t.Helper()
	registry, err := NewRegistry(true)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	model, ok := registry.Get(id)
	if !ok {
		t.Fatal("fal model missing from registry")
	}
	return model
}

func TestFalProviderRendersAndDownloadsVideo(t *testing.T) {
	fake := newFakeFal(t)
	output := filepath.Join(t.TempDir(), "job", "0.mp4")
	var updates []RenderUpdate
	result, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
		Model:      falModel(t),
		OutputPath: output,
	}, func(update RenderUpdate) { updates = append(updates, update) })
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	sum := sha256.Sum256(fakeMP4)
	if result.MIMEType != "video/mp4" || result.FileSize != len(fakeMP4) || result.ChecksumSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected output %+v", result)
	}
	stored, err := os.ReadFile(output)
	if err != nil || string(stored) != string(fakeMP4) {
		t.Fatalf("output file not written: %v", err)
	}
	fake.mu.Lock()
	submitted, auth := fake.submitted, fake.authHeader
	fake.mu.Unlock()
	if auth != "Key test-key" {
		t.Fatalf("authorization header = %q", auth)
	}
	if submitted["image_url"] != "data:image/png;base64,cG5nLWJ5dGVz" {
		t.Fatalf("image must be sent inline, got %v", submitted["image_url"])
	}
	if submitted["num_frames"] != float64(81) || submitted["aspect_ratio"] != "9:16" || submitted["resolution"] != "480p" {
		t.Fatalf("unexpected fal input %v", submitted)
	}
	if len(updates) == 0 || updates[0].ProviderJobID != "req-1" || updates[0].ProviderState["status_url"] == "" {
		t.Fatalf("first update must carry the provider state, got %+v", updates)
	}
}

func TestFalProviderResumesWithoutResubmitting(t *testing.T) {
	fake := newFakeFal(t)
	state := map[string]string{
		"request_id":   "req-1",
		"status_url":   fake.queue.URL + "/fal-ai/wan/requests/req-1/status",
		"response_url": fake.queue.URL + "/fal-ai/wan/requests/req-1",
	}
	_, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:           Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
		Model:         falModel(t),
		ProviderState: state,
		OutputPath:    filepath.Join(t.TempDir(), "0.mp4"),
	}, func(RenderUpdate) {})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if fake.submits.Load() != 0 {
		t.Fatalf("resumed job was submitted again (%d times) and billed twice", fake.submits.Load())
	}
}

func TestFalProviderMapsSubmitErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		code      string
		retryable bool
	}{
		{"exhausted balance", http.StatusForbidden, `{"detail":"User is locked. Reason: Exhausted balance."}`, "INSUFFICIENT_PROVIDER_CREDIT", false},
		{"bad key", http.StatusUnauthorized, `{"detail":"Invalid key"}`, "PROVIDER_AUTH_FAILED", false},
		{"rate limited", http.StatusTooManyRequests, `{}`, "RATE_LIMITED", true},
		{"server error", http.StatusBadGateway, ``, "MODEL_UNAVAILABLE", true},
		{"content policy", http.StatusUnprocessableEntity, `{"detail":[{"type":"content_policy_violation"}]}`, "CONTENT_REJECTED", false},
		{"invalid input", http.StatusUnprocessableEntity, `{"detail":[{"type":"value_error"}]}`, "VALIDATION_FAILED", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeFal(t)
			fake.submitStatus, fake.submitBody = tc.status, tc.body
			_, err := fake.provider().Render(context.Background(), RenderRequest{
				Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
				Model:      falModel(t),
				OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
			}, func(RenderUpdate) {})
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != tc.code || providerErr.Retryable != tc.retryable {
				t.Fatalf("got %v, want %s retryable=%v", err, tc.code, tc.retryable)
			}
		})
	}
}

func TestFalProviderRejectsForeignQueueHost(t *testing.T) {
	fake := newFakeFal(t)
	fake.statusURLHost = "https://attacker.example"
	_, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
		Model:      falModel(t),
		OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
	}, func(RenderUpdate) {})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "PROVIDER_ERROR" {
		t.Fatalf("a status URL on another host must be refused so the key never leaves fal, got %v", err)
	}
}

func TestFalProviderRejectsNonMP4Output(t *testing.T) {
	fake := newFakeFal(t)
	fake.videoBody = []byte("<html>not a video</html>")
	output := filepath.Join(t.TempDir(), "0.mp4")
	_, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
		Model:      falModel(t),
		OutputPath: output,
	}, func(RenderUpdate) {})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "MEDIA_VALIDATION_FAILED" {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatal("an invalid output must not be left on disk")
	}
}

func TestFalProviderCancelsUpstreamWhenContextEnds(t *testing.T) {
	fake := newFakeFal(t)
	fake.blockStatus = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fake.provider().Render(ctx, RenderRequest{
			Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
			Model:      falModel(t),
			OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
		}, func(RenderUpdate) {})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for fake.statusCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if fake.cancels.Load() != 1 {
		t.Fatalf("fal cancel endpoint called %d times, want 1", fake.cancels.Load())
	}
}

func TestFalWanInputClampsFrames(t *testing.T) {
	request := falJobRequest("https://drupal.test/callback", "https://drupal.test/scene.png")
	for durationMS, frames := range map[int]int{500: falWanMinFrames, 5000: 81, 10000: 161, 20000: falWanMaxFrames} {
		request.Output.DurationMS = durationMS
		if got := falWanImageToVideoInput(request, falMedia{Image: "data:"})["num_frames"]; got != frames {
			t.Fatalf("duration %d → %v frames, want %d", durationMS, got, frames)
		}
	}
	request.ProviderOptions = map[string]any{"seed": float64(42), "scenario": "slow"}
	input := falWanImageToVideoInput(request, falMedia{Image: "data:"})
	if input["seed"] != int64(42) {
		t.Fatalf("seed not forwarded: %v", input["seed"])
	}
	if _, leaked := input["scenario"]; leaked {
		t.Fatal("mock scenario must not reach fal")
	}
}

func TestFalAcceptsSceneWithoutPrompt(t *testing.T) {
	request := falJobRequest("https://drupal.test/callback", "https://drupal.test/scene.png")
	empty := "   "
	request.Inputs.Prompt = &empty
	if got := falWanImageToVideoInput(request, falMedia{Image: "data:"})["prompt"]; got != falDefaultMotionPrompt {
		t.Fatalf("empty prompt must fall back to the motion default, got %q", got)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	service := newFalJobService(t, t.TempDir(), server.Client(), &stubProvider{})
	request.Callback.URL = server.URL
	request.Inputs.Prompt = nil
	if _, _, err := service.Create(request, "idem-noprompt", "https://goclaw.test"); err != nil {
		t.Fatalf("a storyboard scene without a visual prompt must still be accepted: %v", err)
	}
}

func TestRegistryListsFalOnlyWithKey(t *testing.T) {
	without, err := NewRegistry(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := without.Get("fal/wan-2.2-a14b-i2v"); ok {
		t.Fatal("fal model listed without a key")
	}
	with, err := NewRegistry(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := with.Get("mock/cinematic-v1"); !ok {
		t.Fatal("mock models must stay listed alongside fal")
	}
	if with.CatalogVersion() == without.CatalogVersion() {
		t.Fatal("adding fal must change the catalog version so Drupal refreshes")
	}
}

// stubProvider stands in for fal inside the job service tests.
type stubProvider struct {
	calls   atomic.Int32
	block   chan struct{}
	lastReq atomic.Pointer[RenderRequest]
}

func (p *stubProvider) Render(ctx context.Context, request RenderRequest, update func(RenderUpdate)) (RenderOutput, error) {
	p.calls.Add(1)
	p.lastReq.Store(&request)
	if request.ProviderState == nil {
		update(RenderUpdate{ProviderJobID: "req-9", ProviderState: map[string]string{"request_id": "req-9"}, Progress: 10, Message: "submitted"})
	}
	if p.block != nil {
		select {
		case <-ctx.Done():
			return RenderOutput{}, ctx.Err()
		case <-p.block:
		}
	}
	if err := os.MkdirAll(filepath.Dir(request.OutputPath), 0o700); err != nil {
		return RenderOutput{}, err
	}
	if err := os.WriteFile(request.OutputPath, fakeMP4, 0o600); err != nil {
		return RenderOutput{}, err
	}
	sum := sha256.Sum256(fakeMP4)
	return RenderOutput{MIMEType: "video/mp4", FileSize: len(fakeMP4), ChecksumSHA256: hex.EncodeToString(sum[:])}, nil
}

func newFalJobService(t *testing.T, dir string, client *http.Client, provider RenderProvider) *JobService {
	t.Helper()
	registry, err := NewRegistry(true)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileJobStore(filepath.Join(dir, "video-jobs"))
	if err != nil {
		t.Fatal(err)
	}
	return NewJobService(registry, store, client, time.Millisecond,
		WithRenderProviders(filepath.Join(dir, "video-outputs"), map[string]RenderProvider{FalProviderName: provider}))
}

func TestJobServiceRoutesFalModelToProvider(t *testing.T) {
	recorder := &callbackRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer server.Close()
	provider := &stubProvider{}
	service := newFalJobService(t, t.TempDir(), server.Client(), provider)

	created, _, err := service.Create(falJobRequest(server.URL, "https://drupal.test/scene.png"), "idem-fal-001", "https://goclaw.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ProviderJobID != nil {
		t.Fatalf("a real provider job must not carry a mock id, got %q", *created.ProviderJobID)
	}
	job := waitForTerminal(t, service, created.ID)
	if job.Status != JobSucceeded || len(job.Outputs) != 1 {
		t.Fatalf("job = %+v", job)
	}
	if job.ProviderJobID == nil || *job.ProviderJobID != "req-9" {
		t.Fatalf("provider job id not recorded: %v", job.ProviderJobID)
	}
	if req := provider.lastReq.Load(); req == nil || req.Model.Provider != FalProviderName {
		t.Fatal("provider did not receive the fal model")
	}
	// Relative so Drupal resolves it against the GoClaw origin it trusts, not whatever the proxy forwarded.
	if !strings.HasPrefix(job.Outputs[0].DownloadURL, "/v1/video/jobs/"+created.ID+"/outputs/0?token=") {
		t.Fatalf("download URL = %s", job.Outputs[0].DownloadURL)
	}

	stored, _ := service.store.Get(created.ID)
	data, mimeType, ok := service.Output(created.ID, 0, stored.OutputToken)
	if !ok || mimeType != "video/mp4" || string(data) != string(fakeMP4) {
		t.Fatal("output endpoint must serve the rendered file, not the mock fixture")
	}
	if _, _, ok := service.Output(created.ID, 0, "wrong-token"); ok {
		t.Fatal("output served without the job token")
	}

	// The Drupal "permanent-failure" mock scenario in the request must be ignored.
	for _, event := range recorder.snapshot() {
		if event.Status == JobFailed {
			t.Fatalf("mock scenario leaked into a real provider job: %+v", event)
		}
	}
}

func TestJobServiceResumesProviderJobAfterRestart(t *testing.T) {
	recorder := &callbackRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer server.Close()
	dir := t.TempDir()
	first := &stubProvider{block: make(chan struct{})}
	service := newFalJobService(t, dir, server.Client(), first)
	created, _, err := service.Create(falJobRequest(server.URL, "https://drupal.test/scene.png"), "idem-fal-002", "https://goclaw.test")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stored, _ := service.store.Get(created.ID); stored.ProviderState["request_id"] == "req-9" {
			break
		}
		time.Sleep(time.Millisecond)
	}

	second := &stubProvider{}
	reloaded := newFalJobService(t, dir, server.Client(), second)
	job := waitForTerminal(t, reloaded, created.ID)
	if job.Status != JobSucceeded {
		t.Fatalf("resumed job = %+v", job)
	}
	if req := second.lastReq.Load(); req == nil || req.ProviderState["request_id"] != "req-9" {
		t.Fatal("restarted service must hand the persisted provider state back to the provider")
	}
	// first stays blocked on purpose: releasing it would race the old store against TempDir cleanup.
}

func TestEveryCallbackCarriesOutputsArray(t *testing.T) {
	var mu sync.Mutex
	var raw []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&event); err == nil {
			mu.Lock()
			raw = append(raw, event)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := newFalJobService(t, t.TempDir(), server.Client(), &stubProvider{})
	mockJob, _, err := service.Create(validJobRequest(server.URL, "success"), "idem-array-01", "https://goclaw.test")
	if err != nil {
		t.Fatal(err)
	}
	falJob, _, err := service.Create(falJobRequest(server.URL, "https://drupal.test/scene.png"), "idem-array-02", "https://goclaw.test")
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, service, mockJob.ID)
	waitForTerminal(t, service, falJob.ID)

	mu.Lock()
	defer mu.Unlock()
	if len(raw) < 4 {
		t.Fatalf("expected progress and completion callbacks, got %d", len(raw))
	}
	for _, event := range raw {
		// Drupal's assertCallback answers 422 unless outputs is an array.
		if outputs := strings.TrimSpace(string(event["outputs"])); !strings.HasPrefix(outputs, "[") {
			t.Fatalf("callback %s sent outputs=%s", event["event_type"], outputs)
		}
	}
}

func TestJobServiceCancelStopsProvider(t *testing.T) {
	recorder := &callbackRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer server.Close()
	provider := &stubProvider{block: make(chan struct{})}
	service := newFalJobService(t, t.TempDir(), server.Client(), provider)
	created, _, err := service.Create(falJobRequest(server.URL, "https://drupal.test/scene.png"), "idem-fal-003", "https://goclaw.test")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for provider.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := service.Cancel(created.ID); err != nil {
		t.Fatal(err)
	}
	job := waitForTerminal(t, service, created.ID)
	if job.Status != JobCancelled {
		t.Fatalf("job = %+v", job)
	}
	// Give the provider goroutine time to observe the cancelled context and exit.
	time.Sleep(20 * time.Millisecond)
	if after, _ := service.Get(created.ID); after.Status != JobCancelled {
		t.Fatalf("cancelled job was overwritten: %+v", after)
	}
}

func TestFalKlingAvatarSendsNarrationAndReportsItsLength(t *testing.T) {
	fake := newFakeFal(t)
	request := falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")
	voice := fake.media.URL + "/voice.wav"
	request.Inputs.AudioURL = &voice
	result, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: request},
		Model:      falModelByID(t, "fal/kling-avatar-v2-pro"),
		OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
	}, func(RenderUpdate) {})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.submitPath != "/fal-ai/kling-video/ai-avatar/v2/pro" {
		t.Fatalf("submitted to %s", fake.submitPath)
	}
	if fake.submitted["audio_url"] != "data:audio/wav;base64,d2F2LWJ5dGVz" || fake.submitted["image_url"] == nil {
		t.Fatalf("avatar input must inline both image and narration, got %v", fake.submitted)
	}
	// The clip follows the narration, so the job must report fal's length, not the requested one.
	if result.DurationMS != 7250 {
		t.Fatalf("duration = %d, want 7250 from fal", result.DurationMS)
	}
}

func TestFalKlingAvatarRefusesSceneWithoutNarration(t *testing.T) {
	fake := newFakeFal(t)
	_, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: falJobRequest("https://drupal.test/callback", fake.media.URL+"/scene.png")},
		Model:      falModelByID(t, "fal/kling-avatar-v2-pro"),
		OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
	}, func(RenderUpdate) {})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "INVALID_REQUEST" || !strings.Contains(providerErr.Message, "Đọc lời") {
		t.Fatalf("got %v", err)
	}
	if fake.submits.Load() != 0 {
		t.Fatal("nothing may be billed when the narration is missing")
	}
}

func TestFalKlingImageToVideoInput(t *testing.T) {
	request := falJobRequest("https://drupal.test/callback", "https://drupal.test/scene.png")
	request.Output.DurationMS = 10000
	input := falKlingImageToVideoInput(request, falMedia{Image: "data:image/png;base64,x"})
	if input["start_image_url"] != "data:image/png;base64,x" || input["duration"] != "10" || input["generate_audio"] != true {
		t.Fatalf("unexpected Kling input %v", input)
	}
	if _, leaked := input["image_url"]; leaked {
		t.Fatal("Kling names the image start_image_url")
	}
	request.Output.DurationMS = 5000
	request.ProviderOptions = map[string]any{"generate_audio": false}
	input = falKlingImageToVideoInput(request, falMedia{Image: "data:"})
	if input["duration"] != "5" || input["generate_audio"] != false {
		t.Fatalf("user must be able to switch sound off: %v", input)
	}
}

func TestEveryFalCatalogModelHasAnInputBuilder(t *testing.T) {
	for _, model := range falModels() {
		if _, ok := falInputBuilders[*model.ProviderModelID]; !ok {
			t.Fatalf("model %s has no input builder; its jobs would fail at submit", model.ID)
		}
	}
}

func TestFalAcceptsInlinedSceneMediaWithoutFetching(t *testing.T) {
	fake := newFakeFal(t)
	request := falJobRequest("https://drupal.test/callback", "data:image/png;base64,cG5nLWJ5dGVz")
	voice := "data:audio/x-wav;base64,d2F2LWJ5dGVz"
	request.Inputs.AudioURL = &voice
	if _, err := fake.provider().Render(context.Background(), RenderRequest{
		Job:        Job{Request: request},
		Model:      falModelByID(t, "fal/kling-avatar-v2-pro"),
		OutputPath: filepath.Join(t.TempDir(), "0.mp4"),
	}, func(RenderUpdate) {}); err != nil {
		t.Fatalf("render: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.submitted["image_url"] != "data:image/png;base64,cG5nLWJ5dGVz" || fake.submitted["audio_url"] != "data:audio/wav;base64,d2F2LWJ5dGVz" {
		t.Fatalf("inlined media must reach fal unchanged (audio type normalised), got %v", fake.submitted)
	}
}

func TestInlineDataURIRejectsWrongTypeOrSize(t *testing.T) {
	for _, raw := range []string{
		"data:text/html;base64,PGgxPg==",
		"data:image/png,not-base64-flagged",
		"data:image/png;base64,***",
		"data:image/png;base64,",
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 11)),
	} {
		if _, err := inlineDataURI(raw, falImageTypes, 10, "bad"); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}
