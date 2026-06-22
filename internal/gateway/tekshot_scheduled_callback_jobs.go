package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	tekshottools "github.com/nextlevelbuilder/goclaw/internal/tekshot"
)

type tekshotScheduledCallbackRequest struct {
	ExternalID    string `json:"external_id"`
	RunAtMS       int64  `json:"run_at_ms"`
	CallbackURL   string `json:"callback_url"`
	CallbackToken string `json:"callback_token"`
	Method        string `json:"method"`
	TimeoutMS     int64  `json:"timeout_ms"`
}

func (s *Server) SetTekshotCronStore(service store.CronStore) {
	s.tekshotCron = service
}

func (s *Server) handleTekshotScheduledCallbackJobs(w http.ResponseWriter, r *http.Request) {
	if !s.hasGatewayBearer(r) {
		writeTekshotJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}
	if s.tekshotCron == nil {
		writeTekshotJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "cron service is not configured",
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

	input, ok := decodeTekshotScheduledCallbackRequest(w, r)
	if !ok {
		return
	}
	job, err := s.createTekshotScheduledCallbackJob(r.Context(), input)
	if err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}
	writeTekshotJSON(w, http.StatusAccepted, map[string]any{
		"ok":  true,
		"job": serializeTekshotScheduledCallbackJob(job, input),
	})
}

func (s *Server) handleTekshotScheduledCallbackJob(w http.ResponseWriter, r *http.Request) {
	if !s.hasGatewayBearer(r) {
		writeTekshotJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}
	if s.tekshotCron == nil {
		writeTekshotJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "cron service is not configured",
		})
		return
	}

	jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/tekshot/scheduled-callback-jobs/"))
	if jobID == "" {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "job id is required",
		})
		return
	}

	switch r.Method {
	case http.MethodPut:
		input, ok := decodeTekshotScheduledCallbackRequest(w, r)
		if !ok {
			return
		}
		_ = s.tekshotCron.RemoveJob(r.Context(), jobID)
		job, err := s.createTekshotScheduledCallbackJob(r.Context(), input)
		if err != nil {
			writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
				"ok":      false,
				"message": err.Error(),
			})
			return
		}
		writeTekshotJSON(w, http.StatusOK, map[string]any{
			"ok":  true,
			"job": serializeTekshotScheduledCallbackJob(job, input),
		})
	case http.MethodDelete:
		if err := s.tekshotCron.RemoveJob(r.Context(), jobID); err != nil && err != store.ErrCronJobNotFound {
			writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
				"ok":      false,
				"message": err.Error(),
			})
			return
		}
		writeTekshotJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"deleted": jobID,
		})
	default:
		writeTekshotJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
	}
}

func decodeTekshotScheduledCallbackRequest(w http.ResponseWriter, r *http.Request) (tekshotScheduledCallbackRequest, bool) {
	var input tekshotScheduledCallbackRequest
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&input); err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "invalid JSON payload",
		})
		return input, false
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.CallbackURL = strings.TrimSpace(input.CallbackURL)
	input.CallbackToken = strings.TrimSpace(input.CallbackToken)
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	if input.Method == "" {
		input.Method = http.MethodPost
	}
	if input.TimeoutMS <= 0 {
		input.TimeoutMS = 30000
	}
	return input, true
}

func (s *Server) createTekshotScheduledCallbackJob(ctx context.Context, input tekshotScheduledCallbackRequest) (*store.CronJob, error) {
	if err := validateTekshotScheduledCallbackRequest(input); err != nil {
		return nil, err
	}
	name := "tekshot scheduled callback"
	if input.ExternalID != "" {
		name += ": " + input.ExternalID
	}
	args := map[string]any{
		"external_id":    input.ExternalID,
		"callback_url":   input.CallbackURL,
		"callback_token": input.CallbackToken,
		"method":         input.Method,
		"timeout_ms":     input.TimeoutMS,
	}
	return s.tekshotCron.AddToolCallJob(ctx, name, input.RunAtMS, tekshottools.ScheduledCallbackToolName, args, "", "tekshot")
}

func validateTekshotScheduledCallbackRequest(input tekshotScheduledCallbackRequest) error {
	if input.RunAtMS <= 0 {
		return fmt.Errorf("run_at_ms is required")
	}
	if input.CallbackURL == "" {
		return fmt.Errorf("callback_url is required")
	}
	parsed, err := url.Parse(input.CallbackURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("callback_url must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("callback_url must use http or https")
	}
	if input.CallbackToken == "" {
		return fmt.Errorf("callback_token is required")
	}
	if input.Method != http.MethodPost {
		return fmt.Errorf("only POST callbacks are supported")
	}
	if input.TimeoutMS <= 0 || input.TimeoutMS > int64((2*time.Minute)/time.Millisecond) {
		return fmt.Errorf("timeout_ms must be between 1 and 120000")
	}
	return nil
}

func serializeTekshotScheduledCallbackJob(job *store.CronJob, input tekshotScheduledCallbackRequest) map[string]any {
	payload := map[string]any{
		"id":           job.ID,
		"name":         job.Name,
		"enabled":      job.Enabled,
		"external_id":  input.ExternalID,
		"run_at_ms":    input.RunAtMS,
		"callback_url": input.CallbackURL,
	}
	if job.State.NextRunAtMS != nil {
		payload["next_run_at_ms"] = *job.State.NextRunAtMS
	}
	return payload
}
