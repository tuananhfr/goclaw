package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	tekshottools "github.com/nextlevelbuilder/goclaw/internal/tekshot"
)

func (s *Server) SetTekshotJobService(service *tekshottools.JobService) {
	s.tekshotJobs = service
}

func (s *Server) handleTekshotJobs(w http.ResponseWriter, r *http.Request) {
	if !s.hasGatewayBearer(r) {
		writeTekshotJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}
	if s.tekshotJobs == nil {
		writeTekshotJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "Tekshot job service is not configured",
		})
		return
	}

	if r.Method != http.MethodPost {
		writeTekshotJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	var input tekshottools.JobCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "invalid JSON payload",
		})
		return
	}
	job, err := s.tekshotJobs.Create(r.Context(), input)
	if err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}
	writeTekshotJSON(w, http.StatusAccepted, map[string]any{
		"ok":  true,
		"job": serializeTekshotJob(job, false),
	})
}

func (s *Server) handleTekshotJob(w http.ResponseWriter, r *http.Request) {
	if !s.hasGatewayBearer(r) {
		writeTekshotJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}
	if s.tekshotJobs == nil {
		writeTekshotJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "Tekshot job service is not configured",
		})
		return
	}
	if r.Method != http.MethodGet {
		writeTekshotJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	rawID := strings.TrimPrefix(r.URL.Path, "/v1/tekshot/jobs/")
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "invalid tekshot job id",
		})
		return
	}
	job, err := s.tekshotJobs.Get(r.Context(), id)
	if err != nil {
		writeTekshotJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}
	if job == nil {
		writeTekshotJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "tekshot job not found",
		})
		return
	}
	writeTekshotJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"job": serializeTekshotJob(job, true),
	})
}

func serializeTekshotJob(job *store.TekshotJob, includeResult bool) map[string]any {
	payload := map[string]any{
		"id":                job.ID.String(),
		"external_job_uuid": job.ExternalJobUUID,
		"workspace_id":      job.WorkspaceID,
		"workspace_uuid":    job.WorkspaceUUID,
		"job_type":          job.JobType,
		"agent_key":         job.AgentKey,
		"session_key":       job.SessionKey,
		"status":            job.Status,
		"progress_message":  job.ProgressMessage,
		"error_message":     job.ErrorMessage,
		"attempt_count":     job.AttemptCount,
		"created_at":        job.CreatedAt,
		"updated_at":        job.UpdatedAt,
		"completed_at":      job.CompletedAt,
	}
	if includeResult && len(job.ResultJSON) > 0 {
		var result any
		if err := json.Unmarshal(job.ResultJSON, &result); err == nil {
			payload["result"] = result
		}
	}
	return payload
}
