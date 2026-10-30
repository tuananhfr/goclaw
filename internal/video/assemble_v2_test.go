package video

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/mediaworker"
)

// Lệch phiên bản gateway/worker: hai bên lên image ở hai lúc khác nhau, nên
// từng cặp phải ra kết quả rõ ràng, không job nào "thành công" mà sai.

func editManifest(t *testing.T) *mediaworker.Manifest {
	t.Helper()
	data, err := os.ReadFile("../mediaworker/testdata/manifest-v2-97c8af86.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest mediaworker.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode v2 manifest: %v", err)
	}
	return &manifest
}

func assembleRequest(callbackURL string, manifest *mediaworker.Manifest) JobRequest {
	request := validJobRequest(callbackURL, "success")
	request.JobKind = "project.assemble"
	request.ModelID, request.Operation = nil, nil
	request.Metadata.SceneUUID, request.Metadata.SceneRevision = nil, nil
	request.Metadata.ProjectUUID = manifest.ProjectUUID
	request.Metadata.ProjectRevision = manifest.ProjectRevision
	request.Inputs = JobInputs{Manifest: manifest}
	request.Output.DurationMS = manifest.DurationMS
	return request
}

// runAgainstWorker gửi một job assemble tới worker giả và trả job khi xong cùng
// body thô gateway đã gửi cho worker.
func runAgainstWorker(t *testing.T, manifest *mediaworker.Manifest, handler func(http.ResponseWriter, []byte)) (Job, []byte) {
	t.Helper()
	received := make(chan []byte, 1)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		received <- body
		handler(w, body)
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
	job, _, err := service.Create(assembleRequest(callback.URL, manifest), "assemble-"+uuid.NewString()[:8], callback.URL)
	if err != nil {
		t.Fatalf("create assemble job: %v", err)
	}
	terminal := waitForTerminal(t, service, job.ID)
	select {
	case body := <-received:
		return terminal, body
	case <-time.After(time.Second):
		t.Fatal("media worker did not receive the job")
	}
	return terminal, nil
}

func okWorker(w http.ResponseWriter, _ []byte) {
	_ = json.NewEncoder(w).Encode(mediaworker.AssembleResponse{
		ContractVersion: 1,
		DownloadURL:     "https://worker.test/v1/outputs/result?token=signed",
		Result:          mediaworker.Result{MIMEType: "video/mp4", FileSize: 4096, Checksum: strings.Repeat("a", 64), Width: 1080, Height: 1920, DurationMS: 36000, Codec: "h264"},
	})
}

func TestAssembleV2ReachesWorkerIntactAndAcceptsContractOne(t *testing.T) {
	terminal, body := runAgainstWorker(t, editManifest(t), okWorker)
	if terminal.Status != JobSucceeded {
		t.Fatalf("v2 job with a worker answering contract 1 must succeed: %#v", terminal)
	}
	for _, expected := range []string{`"contract_version":2`, `"source_start_ms":41000`, `"effect":"grayscale"`, `"fade_in_ms":500`, `"fade_out_ms":500`} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("gateway dropped %s on the way to the worker: %s", expected, body)
		}
	}
}

func TestAssembleV1PayloadToWorkerCarriesNoEditFields(t *testing.T) {
	manifest := &mediaworker.Manifest{
		ContractVersion: 1, ProjectUUID: uuid.NewString(), ProjectRevision: 7, DurationMS: 6000,
		Scenes: []mediaworker.Scene{{UUID: uuid.NewString(), SourceURL: "https://drupal.test/source.jpg", MIMEType: "image/jpeg", DurationMS: 6000}},
		Output: mediaworker.OutputProfile{Width: 1280, Height: 720, FPS: 30, Format: "mp4", VideoCodec: "h264", SubtitleMode: "mux"},
	}
	terminal, body := runAgainstWorker(t, manifest, func(w http.ResponseWriter, body []byte) {
		// Worker CŨ giải mã với DisallowUnknownFields: gateway mới phải gửi job v1 y
		// như trước, không kèm khoá rỗng nào của v2.
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		var legacy struct {
			JobID    string `json:"job_id"`
			Manifest struct {
				ContractVersion int    `json:"contract_version"`
				ProjectUUID     string `json:"project_uuid"`
				ProjectRevision int    `json:"project_revision"`
				DurationMS      int    `json:"duration_ms,omitempty"`
				Scenes          []struct {
					UUID       string `json:"uuid"`
					AssetUUID  string `json:"asset_uuid,omitempty"`
					SourceURL  string `json:"source_url"`
					MIMEType   string `json:"mime_type"`
					DurationMS int    `json:"duration_ms"`
					Motion     string `json:"motion,omitempty"`
					Transition any    `json:"transition,omitempty"`
				} `json:"scenes"`
				Audio     []any `json:"audio,omitempty"`
				Subtitles []any `json:"subtitles,omitempty"`
				Output    any   `json:"output"`
			} `json:"manifest"`
		}
		if err := decoder.Decode(&legacy); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid assemble request"}`))
			return
		}
		okWorker(w, body)
	})
	if terminal.Status != JobSucceeded {
		t.Fatalf("v1 job must still pass an old worker: %#v body=%s", terminal, body)
	}
}

func TestAssembleV2OnOldWorkerFailsWithoutRetry(t *testing.T) {
	terminal, _ := runAgainstWorker(t, editManifest(t), func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid assemble request"}` + "\n"))
	})
	if terminal.Status != JobFailed || terminal.Error == nil || terminal.Error.Code != "MEDIA_ASSEMBLE_FAILED" || terminal.Error.Retryable {
		t.Fatalf("v2 job on a worker that cannot read it must fail MEDIA_ASSEMBLE_FAILED, not retryable: %#v", terminal)
	}
	if len(terminal.Outputs) != 0 {
		t.Fatalf("a failed job must carry no output: %#v", terminal.Outputs)
	}
}

func TestAssembleRejectsWorkerAnsweringAnotherContract(t *testing.T) {
	terminal, _ := runAgainstWorker(t, editManifest(t), func(w http.ResponseWriter, _ []byte) {
		_ = json.NewEncoder(w).Encode(mediaworker.AssembleResponse{
			ContractVersion: 2,
			DownloadURL:     "https://worker.test/v1/outputs/result?token=signed",
			Result:          mediaworker.Result{MIMEType: "video/mp4", FileSize: 4096, Checksum: strings.Repeat("a", 64), Width: 1080, Height: 1920, DurationMS: 36000, Codec: "h264"},
		})
	})
	if terminal.Status != JobFailed || terminal.Error == nil || terminal.Error.Code != "MEDIA_OUTPUT_INVALID" {
		t.Fatalf("the assemble response contract stays 1: %#v", terminal)
	}
}
