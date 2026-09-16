package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/video"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type VideoModelsHandler struct {
	registry video.ModelRegistry
}

type videoModelCatalogResponse struct {
	ContractVersion int           `json:"contract_version"`
	CatalogVersion  string        `json:"catalog_version"`
	Models          []video.Model `json:"models"`
}

func NewVideoModelsHandler(registry video.ModelRegistry) *VideoModelsHandler {
	return &VideoModelsHandler{registry: registry}
}

func (h *VideoModelsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/video/health", requireAuth(permissions.RoleOperator, h.handleHealth))
	mux.HandleFunc("GET /v1/video/models", requireAuth(permissions.RoleOperator, h.handleList))
	mux.HandleFunc("GET /v1/video/models/{model_id...}", requireAuth(permissions.RoleOperator, h.handleGet))
}

func (h *VideoModelsHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	h.writeCatalogHeaders(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"contract_version": video.ContractVersion,
		"catalog_version":  h.registry.CatalogVersion(),
		"status":           "ok",
	})
}

func (h *VideoModelsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	operation := strings.TrimSpace(r.URL.Query().Get("operation"))
	if operation != "" && !video.IsSupportedOperation(operation) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "unsupported video operation")
		return
	}
	status, ok := parseVideoModelStatus(r.URL.Query().Get("status"))
	if !ok {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "unsupported video model status")
		return
	}

	h.writeCatalogHeaders(w)
	writeJSON(w, http.StatusOK, videoModelCatalogResponse{
		ContractVersion: video.ContractVersion,
		CatalogVersion:  h.registry.CatalogVersion(),
		Models: h.registry.List(video.ModelFilter{
			Operation: operation,
			Status:    status,
		}),
	})
}

func (h *VideoModelsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimSpace(r.PathValue("model_id")), "/")
	model, ok := h.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, fmt.Sprintf("video model %q not found", id))
		return
	}
	h.writeCatalogHeaders(w)
	writeJSON(w, http.StatusOK, model)
}

func (h *VideoModelsHandler) writeCatalogHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("ETag", fmt.Sprintf("\"video-models:%s\"", h.registry.CatalogVersion()))
}

func parseVideoModelStatus(raw string) (video.ModelStatus, bool) {
	status := video.ModelStatus(strings.TrimSpace(raw))
	switch status {
	case "", video.ModelAvailable, video.ModelDegraded, video.ModelDisabled, video.ModelDeprecated:
		return status, true
	default:
		return "", false
	}
}
