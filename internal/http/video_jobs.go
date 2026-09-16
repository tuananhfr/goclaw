package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/video"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type VideoJobsHandler struct {
	jobs *video.JobService
}

type videoJobResponse struct {
	ContractVersion int       `json:"contract_version"`
	Job             video.Job `json:"job"`
}

func NewVideoJobsHandler(jobs *video.JobService) *VideoJobsHandler {
	return &VideoJobsHandler{jobs: jobs}
}

func (h *VideoJobsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/video/jobs", requireAuth(permissions.RoleOperator, h.handleCreate))
	mux.HandleFunc("GET /v1/video/jobs/{job_id}", requireAuth(permissions.RoleOperator, h.handleGet))
	mux.HandleFunc("POST /v1/video/jobs/{job_id}/cancel", requireAuth(permissions.RoleOperator, h.handleCancel))
	mux.HandleFunc("GET /v1/video/jobs/{job_id}/outputs/{variant}", h.handleOutput)
}

func (h *VideoJobsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 64 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "Idempotency-Key must contain 8 to 64 characters")
		return
	}
	var request video.JobRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, fmt.Sprintf("invalid video job request: %v", err))
		return
	}
	job, _, err := h.jobs.Create(request, idempotencyKey, requestBaseURL(r))
	if err != nil {
		h.writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, videoJobResponse{ContractVersion: video.ContractVersion, Job: job})
}

func (h *VideoJobsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	job, ok := h.jobs.Get(strings.TrimSpace(r.PathValue("job_id")))
	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, "video job not found")
		return
	}
	writeJSON(w, http.StatusOK, videoJobResponse{ContractVersion: video.ContractVersion, Job: job})
}

func (h *VideoJobsHandler) handleCancel(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 64 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "Idempotency-Key must contain 8 to 64 characters")
		return
	}
	job, err := h.jobs.Cancel(strings.TrimSpace(r.PathValue("job_id")))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, "video job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "could not cancel video job")
		return
	}
	writeJSON(w, http.StatusOK, videoJobResponse{ContractVersion: video.ContractVersion, Job: job})
}

func (h *VideoJobsHandler) handleOutput(w http.ResponseWriter, r *http.Request) {
	variant, err := strconv.Atoi(r.PathValue("variant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, mimeType, ok := h.jobs.Output(r.PathValue("job_id"), variant, r.URL.Query().Get("token"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		return
	}
}

func (h *VideoJobsHandler) writeJobError(w http.ResponseWriter, err error) {
	var apiError *video.JobAPIError
	if errors.As(err, &apiError) {
		writeError(w, apiError.Status, apiError.Code, apiError.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "could not persist video job")
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}
