package video

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type callbackRecorder struct {
	mu     sync.Mutex
	events []CallbackEvent
}

func (r *callbackRecorder) handler(w http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var event CallbackEvent
	if err := json.NewDecoder(request.Body).Decode(&event); err == nil {
		r.mu.Lock()
		r.events = append(r.events, event)
		r.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (r *callbackRecorder) snapshot() []CallbackEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CallbackEvent(nil), r.events...)
}

func validJobRequest(callbackURL, scenario string) JobRequest {
	modelID, operation := "mock/cinematic-v1", "text-to-video"
	sceneID := uuid.NewString()
	sceneRevision := 1
	prompt := "A cinematic product shot"
	return JobRequest{
		ContractVersion: ContractVersion,
		ExternalJobUUID: uuid.NewString(),
		ExternalUserID:  "user-1",
		Scope:           JobScope{Type: "store", ID: uuid.NewString()},
		JobKind:         "scene.render",
		ModelID:         &modelID,
		Operation:       &operation,
		Inputs:          JobInputs{Prompt: &prompt, ReferenceURLs: []string{}},
		Output:          JobOutputRequest{DurationMS: 5000, AspectRatio: "16:9", Resolution: "720p", Count: 1, Format: "mp4"},
		ProviderOptions: map[string]any{"scenario": scenario},
		Callback:        JobCallback{URL: callbackURL, Token: "1234567890123456789012345678901234567890123"},
		Metadata: JobRequestMetadata{
			ProjectUUID: uuid.NewString(), ProjectRevision: 1, SceneUUID: &sceneID,
			SceneRevision: &sceneRevision, RequestID: uuid.NewString(),
		},
	}
}

func waitForTerminal(t *testing.T, service *JobService, id string) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := service.Get(id)
		if ok && isTerminal(job.Status) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not become terminal", id)
	return Job{}
}

func TestJobServicePersistsAndReplaysIdempotentJob(t *testing.T) {
	recorder := &callbackRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "video-jobs")
	store, err := NewFileJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := NewJobService(MustMockRegistry(), store, server.Client(), time.Millisecond)
	request := validJobRequest(server.URL, "success")
	job, duplicate, err := service.Create(request, "same-key-001", server.URL)
	if err != nil || duplicate {
		t.Fatalf("create: duplicate=%v err=%v", duplicate, err)
	}
	terminal := waitForTerminal(t, service, job.ID)
	if terminal.Status != JobSucceeded || len(terminal.Outputs) != 1 {
		t.Fatalf("terminal job = %#v", terminal)
	}

	reloadedStore, err := NewFileJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := NewJobService(MustMockRegistry(), reloadedStore, server.Client(), time.Millisecond)
	persisted, ok := reloaded.Get(job.ID)
	if !ok || persisted.Status != JobSucceeded || len(persisted.Outputs) != 1 {
		t.Fatalf("persisted job = %#v, ok=%v", persisted, ok)
	}
	replayed, duplicate, err := reloaded.Create(request, "same-key-001", server.URL)
	if err != nil || !duplicate || replayed.ID != job.ID {
		t.Fatalf("idempotent replay = %#v duplicate=%v err=%v", replayed, duplicate, err)
	}
	changed := request
	changed.Output.Count = 2
	_, _, err = reloaded.Create(changed, "same-key-001", server.URL)
	var apiError *JobAPIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusConflict {
		t.Fatalf("idempotency conflict error = %v", err)
	}
}

func TestValidateStructuredWorkflowImageReferences(t *testing.T) {
	service := &JobService{registry: MustMockRegistry()}
	request := validJobRequest("https://tekshot.example/callback", "success")
	modelID, operation := "mock/reference-v1", "reference-to-video"
	request.ModelID = &modelID
	request.Operation = &operation
	request.Output.DurationMS = 6000
	request.Output.Resolution = "1080p"
	request.Inputs.ReferenceURLs = []string{"https://tekshot.example/person.jpg", "https://tekshot.example/product.jpg"}
	request.Inputs.ReferenceInputs = []ImageReferenceInput{
		{URL: request.Inputs.ReferenceURLs[0], Name: "Người mẫu", Description: "Giữ nguyên khuôn mặt."},
		{URL: request.Inputs.ReferenceURLs[1], Name: "Sản phẩm", Description: "Giữ đúng màu sắc."},
	}

	if err := service.validateRequest(request); err != nil {
		t.Fatalf("structured references should be valid: %v", err)
	}

	request.Inputs.ReferenceInputs[1].URL = "https://tekshot.example/other.jpg"
	if err := service.validateRequest(request); err == nil {
		t.Fatal("mismatched legacy and structured references should be rejected")
	}
}

func TestMockScenarioMatrix(t *testing.T) {
	tests := []struct {
		scenario string
		status   JobStatus
	}{
		{"success", JobSucceeded},
		{"slow", JobSucceeded},
		{"transient-failure", JobSucceeded},
		{"permanent-failure", JobFailed},
		{"timeout", JobFailed},
		{"callback-lost", JobSucceeded},
		{"duplicate-callback", JobSucceeded},
		{"out-of-order-callback", JobSucceeded},
		{"cancel-before-start", JobCancelled},
		{"invalid-output", JobSucceeded},
	}
	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			recorder := &callbackRecorder{}
			server := httptest.NewServer(http.HandlerFunc(recorder.handler))
			defer server.Close()
			store, err := NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
			if err != nil {
				t.Fatal(err)
			}
			service := NewJobService(MustMockRegistry(), store, server.Client(), time.Millisecond)
			job, _, err := service.Create(validJobRequest(server.URL, test.scenario), "scenario-"+test.scenario, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			terminal := waitForTerminal(t, service, job.ID)
			if terminal.Status != test.status {
				t.Fatalf("status = %s, want %s", terminal.Status, test.status)
			}
			time.Sleep(20 * time.Millisecond)
			events := recorder.snapshot()
			if test.scenario == "callback-lost" {
				for _, event := range events {
					if event.Status == JobSucceeded {
						t.Fatal("callback-lost delivered its terminal callback")
					}
				}
			}
			if test.scenario == "duplicate-callback" {
				seen := make(map[string]int)
				for _, event := range events {
					seen[event.EventID]++
				}
				duplicateFound := false
				for _, count := range seen {
					duplicateFound = duplicateFound || count > 1
				}
				if !duplicateFound {
					t.Fatal("duplicate-callback did not repeat an event_id")
				}
			}
			if test.scenario == "out-of-order-callback" {
				outOfOrder := false
				for index := 1; index < len(events); index++ {
					outOfOrder = outOfOrder || events[index].Sequence < events[index-1].Sequence
				}
				if !outOfOrder {
					t.Fatal("out-of-order-callback remained monotonic")
				}
			}
		})
	}
}

func TestMockMultipleOutputsAndCreationFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	store, err := NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewJobService(MustMockRegistry(), store, server.Client(), time.Millisecond)
	request := validJobRequest(server.URL, "multiple-outputs")
	modelID, operation := "mock/reference-v1", "reference-to-video"
	request.ModelID, request.Operation = &modelID, &operation
	request.Output.Resolution, request.Output.Count = "1080p", 4
	job, _, err := service.Create(request, "multiple-output-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if terminal := waitForTerminal(t, service, job.ID); len(terminal.Outputs) != 4 {
		t.Fatalf("outputs = %d, want 4", len(terminal.Outputs))
	}

	unavailable := validJobRequest(server.URL, "provider-unavailable")
	_, _, err = service.Create(unavailable, "provider-unavailable-key", server.URL)
	var apiError *JobAPIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("provider unavailable error = %v", err)
	}
	disabled := validJobRequest(server.URL, "success")
	disabledID := "mock/disabled-v1"
	disabled.ModelID = &disabledID
	_, _, err = service.Create(disabled, "disabled-model-key", server.URL)
	if !errors.As(err, &apiError) || apiError.Code != "MODEL_DISABLED" {
		t.Fatalf("disabled model error = %v", err)
	}
}

func TestCancelRaceKeepsFirstTerminalState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	store, err := NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewJobService(MustMockRegistry(), store, server.Client(), 20*time.Millisecond)
	job, _, err := service.Create(validJobRequest(server.URL, "cancel-race-success"), "cancel-race-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(job.ID)
	if err != nil || cancelled.Status != JobCancelled {
		t.Fatalf("cancelled = %#v err=%v", cancelled, err)
	}
	time.Sleep(80 * time.Millisecond)
	current, _ := service.Get(job.ID)
	if current.Status != JobCancelled {
		t.Fatalf("late success overwrote first terminal state: %s", current.Status)
	}
}

func TestCallbackDeliveryRetriesWithStableEventID(t *testing.T) {
	requests := 0
	eventIDs := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var event CallbackEvent
		if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
			t.Fatal(err)
		}
		requests++
		eventIDs = append(eventIDs, event.EventID)
		if requests < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewJobService(MustMockRegistry(), store, server.Client(), time.Millisecond)
	job := Job{
		ID: "job-callback-retry",
		Request: JobRequest{Callback: JobCallback{
			URL: server.URL, Token: "1234567890123456789012345678901234567890123",
		}},
	}
	event := CallbackEvent{EventID: "stable-event-id"}
	service.deliver(job, event)

	if requests != 3 {
		t.Fatalf("callback requests = %d, want 3", requests)
	}
	for _, eventID := range eventIDs {
		if eventID != event.EventID {
			t.Fatalf("retry event ID = %q, want %q", eventID, event.EventID)
		}
	}
}
