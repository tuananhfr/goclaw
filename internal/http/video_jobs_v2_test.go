package http

import (
	"bytes"
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
	"github.com/nextlevelbuilder/goclaw/internal/video"
)

// Drupal "Xuất video đã chỉnh" gửi job lớp ngoài contract_version 1 với manifest
// contract_version 2 (snapshot thật của job 97c8af86, đã che chữ ký/token).
// Gateway cũ trả 400 'unknown field "effect"' ngay ở bước giải mã này.

func newVideoJobsServer(t *testing.T) *httptest.Server {
	t.Helper()
	setupTestToken(t, "")
	// Không có worker: job sẽ hỏng sau, nhưng ở đây chỉ đo bước nhận job.
	t.Setenv("GOCLAW_MEDIA_WORKER_URL", "")
	t.Setenv("GOCLAW_MEDIA_WORKER_TOKEN", "")
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(callback.Close)
	store, err := video.NewFileJobStore(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	service := video.NewJobService(video.MustMockRegistry(), store, callback.Client(), time.Millisecond)
	mux := http.NewServeMux()
	NewVideoJobsHandler(service).RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// editSnapshot trả body của job 97c8af86 với id, callback URL và token hợp lệ.
func editSnapshot(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/video-job-97c8af86.json")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	body["external_job_uuid"] = uuid.NewString()
	body["callback"] = map[string]any{"url": "http://callback.test/api/tekshot-video/jobs/x/callback", "token": strings.Repeat("k", 43)}
	return body
}

func editScenes(body map[string]any) []any {
	return body["inputs"].(map[string]any)["manifest"].(map[string]any)["scenes"].([]any)
}

func postVideoJob(t *testing.T, server *httptest.Server, body map[string]any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/video/jobs", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if decoded == nil {
		decoded = map[string]any{"raw": string(raw)}
	}
	return response.StatusCode, decoded
}

func errorOf(decoded map[string]any) (string, string) {
	failure, _ := decoded["error"].(map[string]any)
	code, _ := failure["code"].(string)
	message, _ := failure["message"].(string)
	return code, message
}

func TestVideoJobsAcceptsDrupalEditSnapshot(t *testing.T) {
	server := newVideoJobsServer(t)
	status, decoded := postVideoJob(t, server, editSnapshot(t))
	if status != http.StatusAccepted {
		t.Fatalf("snapshot 97c8af86 must be accepted with 202, got %d: %v", status, decoded)
	}
	// Lớp ngoài vẫn là contract 1: Drupal đòi === 1 ở phản hồi job.
	if decoded["contract_version"] != float64(1) {
		t.Fatalf("job response must keep contract_version 1: %v", decoded)
	}
}

func TestVideoJobsStillRejectsUnknownKeys(t *testing.T) {
	server := newVideoJobsServer(t)

	body := editSnapshot(t)
	editScenes(body)[0].(map[string]any)["foo"] = 1
	if status, decoded := postVideoJob(t, server, body); status != http.StatusBadRequest {
		t.Fatalf("unknown scene key must stay 400, got %d: %v", status, decoded)
	}

	body = editSnapshot(t)
	body["metadata"].(map[string]any)["post_production"].(map[string]any)["render_hint"] = "x"
	if status, decoded := postVideoJob(t, server, body); status != http.StatusBadRequest {
		t.Fatalf("unknown post_production key must stay 400, got %d: %v", status, decoded)
	}
}

func TestVideoJobsRejectsEditFieldsOnV1ManifestByName(t *testing.T) {
	server := newVideoJobsServer(t)
	for _, field := range []string{"source_start_ms", "effect", "fade_in_ms", "fade_out_ms"} {
		body := editSnapshot(t)
		manifest := body["inputs"].(map[string]any)["manifest"].(map[string]any)
		manifest["contract_version"] = 1
		for _, raw := range editScenes(body) {
			scene := raw.(map[string]any)
			for _, other := range []string{"source_start_ms", "effect", "fade_in_ms", "fade_out_ms"} {
				if other != field {
					delete(scene, other)
				}
			}
		}
		scene := editScenes(body)[1].(map[string]any)
		if _, ok := scene[field]; !ok {
			// Kể cả giá trị "vô hại" (0, "none") cũng phải bị từ chối ở v1.
			scene[field] = 0
			if field == "effect" {
				scene[field] = "none"
			}
		}
		status, decoded := postVideoJob(t, server, body)
		code, message := errorOf(decoded)
		if status != http.StatusUnprocessableEntity || code != "INVALID_REQUEST" || !strings.Contains(message, field) {
			t.Fatalf("v1 manifest with %s must be 422 INVALID_REQUEST naming it, got %d %s %q", field, status, code, message)
		}
	}

	body := editSnapshot(t)
	body["inputs"].(map[string]any)["manifest"].(map[string]any)["contract_version"] = 3
	status, decoded := postVideoJob(t, server, body)
	code, message := errorOf(decoded)
	if status != http.StatusUnprocessableEntity || code != "INVALID_REQUEST" || !strings.Contains(message, "contract_version 3") {
		t.Fatalf("manifest contract_version 3 must be 422 naming it, got %d %s %q", status, code, message)
	}
}
