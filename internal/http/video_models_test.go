package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/video"
)

func newVideoModelsMux(t *testing.T) *http.ServeMux {
	t.Helper()
	setupTestToken(t, "")
	mux := http.NewServeMux()
	NewVideoModelsHandler(video.MustMockRegistry()).RegisterRoutes(mux)
	return mux
}

func TestVideoModelsHandlerListsAndFiltersCatalog(t *testing.T) {
	mux := newVideoModelsMux(t)
	req := httptest.NewRequest("GET", "/v1/video/models?operation=reference-to-video&status=degraded", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response videoModelCatalogResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ContractVersion != 1 || response.CatalogVersion == "" {
		t.Fatalf("invalid catalog metadata: %#v", response)
	}
	if len(response.Models) != 1 || response.Models[0].ID != "mock/reference-v1" {
		t.Fatalf("models = %#v", response.Models)
	}
	if rr.Header().Get("ETag") == "" {
		t.Fatal("missing catalog ETag")
	}
}

func TestVideoModelsHandlerGetsSlashModelID(t *testing.T) {
	mux := newVideoModelsMux(t)
	req := httptest.NewRequest("GET", "/v1/video/models/mock/cinematic-v1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var model video.Model
	if err := json.NewDecoder(rr.Body).Decode(&model); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if model.ID != "mock/cinematic-v1" {
		t.Fatalf("model id = %q", model.ID)
	}
}

func TestVideoModelsHandlerRejectsUnknownFilter(t *testing.T) {
	mux := newVideoModelsMux(t)
	req := httptest.NewRequest("GET", "/v1/video/models?status=imaginary", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}
