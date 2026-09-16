package video

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/mediaworker"
)

func TestProjectAssembleDelegatesImmutableManifestToMediaWorker(t *testing.T) {
	workerRequests := make(chan mediaworker.AssembleRequest, 1)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer worker-secret-token-with-at-least-32-characters" {
			t.Errorf("unexpected authorization header: %q", request.Header.Get("Authorization"))
		}
		var payload mediaworker.AssembleRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode worker request: %v", err)
		}
		workerRequests <- payload
		_ = json.NewEncoder(w).Encode(mediaworker.AssembleResponse{
			ContractVersion: 1,
			DownloadURL:     "https://worker.test/v1/outputs/result?token=signed",
			Result:          mediaworker.Result{MIMEType: "video/mp4", FileSize: 4096, Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Width: 1280, Height: 720, DurationMS: 6000, Codec: "h264"},
		})
	}))
	defer worker.Close()
	t.Setenv("GOCLAW_MEDIA_WORKER_URL", worker.URL)
	t.Setenv("GOCLAW_MEDIA_WORKER_TOKEN", "worker-secret-token-with-at-least-32-characters")

	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer callback.Close()
	store, err := NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewJobService(MustMockRegistry(), store, worker.Client(), time.Millisecond)
	manifest := mediaworker.Manifest{
		ContractVersion: 1,
		ProjectUUID:     uuid.NewString(),
		ProjectRevision: 7,
		Scenes:          []mediaworker.Scene{{UUID: uuid.NewString(), SourceURL: "https://drupal.test/source.jpg", MIMEType: "image/jpeg", DurationMS: 6000}},
		Output:          mediaworker.OutputProfile{Width: 1280, Height: 720, FPS: 30, Format: "mp4", VideoCodec: "h264", SubtitleMode: "mux"},
	}
	request := validJobRequest(callback.URL, "success")
	request.JobKind = "project.assemble"
	request.ModelID, request.Operation = nil, nil
	request.Metadata.SceneUUID, request.Metadata.SceneRevision = nil, nil
	request.Metadata.ProjectUUID = manifest.ProjectUUID
	request.Metadata.ProjectRevision = manifest.ProjectRevision
	request.Inputs = JobInputs{Manifest: &manifest}
	request.Output.DurationMS = 6000

	job, _, err := service.Create(request, "assemble-idempotency-key", callback.URL)
	if err != nil {
		t.Fatalf("create assemble job: %v", err)
	}
	terminal := waitForTerminal(t, service, job.ID)
	if terminal.Status != JobSucceeded || len(terminal.Outputs) != 1 {
		t.Fatalf("terminal assemble job = %#v", terminal)
	}
	if terminal.Outputs[0].DownloadURL != "https://worker.test/v1/outputs/result?token=signed" || terminal.Outputs[0].DurationMS == nil || *terminal.Outputs[0].DurationMS != 6000 {
		t.Fatalf("unexpected assemble output = %#v", terminal.Outputs[0])
	}
	select {
	case received := <-workerRequests:
		if received.JobID != job.ID || received.Manifest.ProjectRevision != 7 || len(received.Manifest.Scenes) != 1 {
			t.Fatalf("worker request = %#v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("media worker did not receive assemble request")
	}
}
