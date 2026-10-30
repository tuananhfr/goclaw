package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/video"
)

// Catalog và health CHỈ THÊM assemble_manifest_versions; contract_version vẫn 1
// (Drupal fetchCatalog đòi === 1). Giá trị theo /health của media worker thật,
// để Drupal không gửi manifest v2 tới worker cũ.

func fakeMediaWorker(t *testing.T, delay time.Duration, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(worker.Close)
	return worker, &hits
}

func catalogVersions(t *testing.T, workerURL string) (map[string]any, map[string]any, *http.ServeMux) {
	t.Helper()
	setupTestToken(t, "")
	t.Setenv("GOCLAW_MEDIA_WORKER_URL", workerURL)
	mux := http.NewServeMux()
	NewVideoModelsHandler(video.MustMockRegistry()).RegisterRoutes(mux)
	get := func(path string) map[string]any {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, recorder.Code, recorder.Body.String())
		}
		var decoded map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	return get("/v1/video/models"), get("/v1/video/health"), mux
}

func manifestVersionsOf(t *testing.T, decoded map[string]any) []int {
	t.Helper()
	if decoded["contract_version"] != float64(1) {
		t.Fatalf("contract_version must stay 1: %v", decoded)
	}
	raw, ok := decoded["assemble_manifest_versions"].([]any)
	if !ok {
		t.Fatalf("missing assemble_manifest_versions: %v", decoded)
	}
	versions := make([]int, 0, len(raw))
	for _, value := range raw {
		versions = append(versions, int(value.(float64)))
	}
	return versions
}

func TestVideoCatalogReportsV2WhenWorkerSupportsIt(t *testing.T) {
	worker, hits := fakeMediaWorker(t, 0, http.StatusOK, `{"ok":true,"contract_version":1,"assemble_manifest_versions":[1,2]}`)
	models, health, mux := catalogVersions(t, worker.URL)
	for name, decoded := range map[string]map[string]any{"models": models, "health": health} {
		if versions := manifestVersionsOf(t, decoded); !slices.Equal(versions, []int{1, 2}) {
			t.Fatalf("%s: assemble_manifest_versions = %v, want [1 2]", name, versions)
		}
	}
	if _, ok := models["models"].([]any); !ok || models["catalog_version"] == "" {
		t.Fatalf("catalog shape changed: %v", models)
	}
	// Có nhớ đệm: hai lần hỏi trên chỉ gọi worker một lần, hỏi thêm cũng vậy.
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/video/models", nil))
	if hits.Load() != 1 {
		t.Fatalf("worker /health called %d times, want 1 (cached)", hits.Load())
	}
}

func TestVideoCatalogFallsBackToV1(t *testing.T) {
	oldWorker, _ := fakeMediaWorker(t, 0, http.StatusOK, `{"ok":true,"contract_version":1}`)
	broken, _ := fakeMediaWorker(t, 0, http.StatusInternalServerError, `{"ok":false}`)
	garbage, _ := fakeMediaWorker(t, 0, http.StatusOK, `not json`)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	for name, workerURL := range map[string]string{"old worker": oldWorker.URL, "worker error": broken.URL, "garbage": garbage.URL, "unreachable": goneURL, "not configured": ""} {
		models, health, _ := catalogVersions(t, workerURL)
		for kind, decoded := range map[string]map[string]any{"models": models, "health": health} {
			if versions := manifestVersionsOf(t, decoded); !slices.Equal(versions, []int{1}) {
				t.Fatalf("%s %s: assemble_manifest_versions = %v, want [1]", name, kind, versions)
			}
		}
	}
}

func TestVideoCatalogDoesNotWaitLongForWorker(t *testing.T) {
	slow, _ := fakeMediaWorker(t, 3*time.Second, http.StatusOK, `{"ok":true,"contract_version":1,"assemble_manifest_versions":[1,2]}`)
	started := time.Now()
	models, _, _ := catalogVersions(t, slow.URL)
	elapsed := time.Since(started)
	if versions := manifestVersionsOf(t, models); !slices.Equal(versions, []int{1}) {
		t.Fatalf("a worker slower than 2 s must count as v1 only, got %v", versions)
	}
	// Drupal hỏi catalog với timeout 8 s: models + health không được vượt ~2 s mỗi lần.
	if elapsed > 5*time.Second {
		t.Fatalf("catalog took %s waiting for the worker", elapsed)
	}
}
