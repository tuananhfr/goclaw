package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/video"
)

func TestVideoJobsHandlerCreateReplayGetCancelAndOutput(t *testing.T) {
	setupTestToken(t, "")
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		_, _ = io.Copy(io.Discard, request.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()
	store, err := video.NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := video.NewJobService(video.MustMockRegistry(), store, callback.Client(), time.Millisecond)
	mux := http.NewServeMux()
	NewVideoJobsHandler(service).RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	modelID, operation, prompt := "mock/cinematic-v1", "text-to-video", "A cinematic product video"
	sceneUUID, sceneRevision := uuid.NewString(), 1
	requestBody := video.JobRequest{
		ContractVersion: video.ContractVersion,
		ExternalJobUUID: uuid.NewString(),
		ExternalUserID:  "user-1",
		Scope:           video.JobScope{Type: "store", ID: uuid.NewString()},
		JobKind:         "scene.render",
		ModelID:         &modelID,
		Operation:       &operation,
		Inputs:          video.JobInputs{Prompt: &prompt},
		Output:          video.JobOutputRequest{DurationMS: 5000, AspectRatio: "16:9", Resolution: "720p", Count: 1},
		ProviderOptions: map[string]any{"scenario": "success"},
		Callback: video.JobCallback{
			URL: callback.URL, Token: "1234567890123456789012345678901234567890123",
		},
		Metadata: video.JobRequestMetadata{
			ProjectUUID: uuid.NewString(), SceneUUID: &sceneUUID,
			ProjectRevision: 1, SceneRevision: &sceneRevision, RequestID: uuid.NewString(),
		},
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	create := func() videoJobResponse {
		t.Helper()
		request, requestErr := http.NewRequest(http.MethodPost, server.URL+"/v1/video/jobs", bytes.NewReader(payload))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "handler-create-key")
		response, requestErr := server.Client().Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("create status = %d, body = %s", response.StatusCode, body)
		}
		var decoded videoJobResponse
		if decodeErr := json.NewDecoder(response.Body).Decode(&decoded); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return decoded
	}
	created := create()
	replayed := create()
	if replayed.Job.ID != created.Job.ID {
		t.Fatalf("idempotent replay job = %q, want %q", replayed.Job.ID, created.Job.ID)
	}

	deadline := time.Now().Add(2 * time.Second)
	var terminal video.Job
	for time.Now().Before(deadline) {
		response, requestErr := server.Client().Get(server.URL + "/v1/video/jobs/" + created.Job.ID)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		var snapshot videoJobResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
		response.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		terminal = snapshot.Job
		if terminal.Status == video.JobSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if terminal.Status != video.JobSucceeded || len(terminal.Outputs) != 1 {
		t.Fatalf("terminal job = %#v", terminal)
	}
	output, err := server.Client().Get(terminal.Outputs[0].DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Body.Close()
	if output.StatusCode != http.StatusOK || output.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("output status = %d, type = %q", output.StatusCode, output.Header.Get("Content-Type"))
	}

	cancel, err := http.NewRequest(http.MethodPost, server.URL+"/v1/video/jobs/"+created.Job.ID+"/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel.Header.Set("Idempotency-Key", "handler-cancel-key")
	cancelResponse, err := server.Client().Do(cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelResponse.Body.Close()
	if cancelResponse.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d", cancelResponse.StatusCode)
	}
}
