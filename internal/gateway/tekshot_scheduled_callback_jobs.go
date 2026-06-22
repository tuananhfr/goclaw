package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	PostID        string `json:"post_id"`
	WorkspaceID   string `json:"workspace_id"`
	TargetPageID  string `json:"target_page_id"`
	PageName      string `json:"page_name"`
	PostTitle     string `json:"post_title"`
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
	job, err := s.upsertTekshotScheduledCallbackJob(r.Context(), input)
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
		job, err := s.updateTekshotScheduledCallbackJob(r.Context(), jobID, input)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrCronJobNotFound) {
				status = http.StatusNotFound
			}
			writeTekshotJSON(w, status, map[string]any{
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
	input.PostID = strings.TrimSpace(input.PostID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.TargetPageID = strings.TrimSpace(input.TargetPageID)
	input.PageName = strings.TrimSpace(input.PageName)
	input.PostTitle = strings.TrimSpace(input.PostTitle)
	if input.Method == "" {
		input.Method = http.MethodPost
	}
	if input.TimeoutMS <= 0 {
		input.TimeoutMS = 30000
	}
	return input, true
}

func (s *Server) upsertTekshotScheduledCallbackJob(ctx context.Context, input tekshotScheduledCallbackRequest) (*store.CronJob, error) {
	if err := validateTekshotScheduledCallbackRequest(input, true); err != nil {
		return nil, err
	}
	matches := s.findTekshotScheduledCallbackJobs(ctx, input.ExternalID)
	if len(matches) > 0 {
		job, err := s.updateTekshotScheduledCallbackJob(ctx, matches[0].ID, input)
		if err != nil {
			return nil, err
		}
		s.removeDuplicateTekshotScheduledCallbackJobs(ctx, input.ExternalID, job.ID)
		return job, nil
	}
	job, err := s.createTekshotScheduledCallbackJob(ctx, input)
	if err != nil {
		return nil, err
	}
	s.removeDuplicateTekshotScheduledCallbackJobs(ctx, input.ExternalID, job.ID)
	return job, nil
}

func (s *Server) createTekshotScheduledCallbackJob(ctx context.Context, input tekshotScheduledCallbackRequest) (*store.CronJob, error) {
	if err := validateTekshotScheduledCallbackRequest(input, true); err != nil {
		return nil, err
	}
	args := tekshotScheduledCallbackArgs(input)
	args["callback_token"] = input.CallbackToken
	return s.tekshotCron.AddToolCallJob(ctx, tekshotScheduledCallbackName(input), input.RunAtMS, tekshottools.ScheduledCallbackToolName, args, "", "tekshot")
}

func (s *Server) updateTekshotScheduledCallbackJob(ctx context.Context, jobID string, input tekshotScheduledCallbackRequest) (*store.CronJob, error) {
	if err := validateTekshotScheduledCallbackRequest(input, false); err != nil {
		return nil, err
	}
	args := tekshotScheduledCallbackArgs(input)
	if input.CallbackToken != "" {
		args["callback_token"] = input.CallbackToken
	}
	schedule := store.CronSchedule{Kind: "at", AtMS: &input.RunAtMS}
	job, err := s.tekshotCron.UpdateJob(ctx, jobID, store.CronJobPatch{
		Name:     tekshotScheduledCallbackName(input),
		Schedule: &schedule,
		ToolArgs: args,
	})
	if err != nil {
		return nil, err
	}
	s.removeDuplicateTekshotScheduledCallbackJobs(ctx, input.ExternalID, job.ID)
	return job, nil
}

func tekshotScheduledCallbackArgs(input tekshotScheduledCallbackRequest) map[string]any {
	args := map[string]any{
		"external_id":  input.ExternalID,
		"callback_url": input.CallbackURL,
		"method":       input.Method,
		"timeout_ms":   input.TimeoutMS,
	}
	if input.PostID != "" {
		args["post_id"] = input.PostID
	}
	if input.WorkspaceID != "" {
		args["workspace_id"] = input.WorkspaceID
	}
	if input.TargetPageID != "" {
		args["target_page_id"] = input.TargetPageID
	}
	if input.PageName != "" {
		args["page_name"] = input.PageName
	}
	if input.PostTitle != "" {
		args["post_title"] = input.PostTitle
	}
	return args
}

func tekshotScheduledCallbackName(input tekshotScheduledCallbackRequest) string {
	page := input.PageName
	if page == "" && input.TargetPageID != "" {
		page = "page " + input.TargetPageID
	}
	if page == "" {
		page = "page unknown"
	}
	postID := input.PostID
	if postID == "" {
		postID = strings.TrimPrefix(input.ExternalID, "tekshot-post-")
	}
	if postID == "" {
		postID = input.ExternalID
	}
	name := "tekshot | " + page + " | post " + postID
	if title := shortenTekshotScheduledCallbackTitle(input.PostTitle, 48); title != "" {
		name += " | " + title
	}
	return name
}

func shortenTekshotScheduledCallbackTitle(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func (s *Server) findTekshotScheduledCallbackJobs(ctx context.Context, externalID string) []store.CronJob {
	if externalID == "" {
		return nil
	}
	jobs := s.tekshotCron.ListJobs(ctx, true, "", "tekshot")
	matches := make([]store.CronJob, 0)
	for _, job := range jobs {
		if job.Payload.Kind != "tool_call" || job.Payload.ToolName != tekshottools.ScheduledCallbackToolName {
			continue
		}
		if scheduledCallbackArgString(job.Payload.Args, "external_id") == externalID {
			matches = append(matches, job)
		}
	}
	return matches
}

func (s *Server) removeDuplicateTekshotScheduledCallbackJobs(ctx context.Context, externalID, keepID string) {
	for _, job := range s.findTekshotScheduledCallbackJobs(ctx, externalID) {
		if job.ID == keepID {
			continue
		}
		_ = s.tekshotCron.RemoveJob(ctx, job.ID)
	}
}

func scheduledCallbackArgString(args map[string]any, key string) string {
	switch value := args[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
	}
	return ""
}

func validateTekshotScheduledCallbackRequest(input tekshotScheduledCallbackRequest, requireToken bool) error {
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
	if requireToken && input.CallbackToken == "" {
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
