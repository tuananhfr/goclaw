package tekshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const ScheduledCallbackToolName = "tekshot_scheduled_callback"

type ScheduledCallbackTool struct{}

func NewScheduledCallbackTool() *ScheduledCallbackTool {
	return &ScheduledCallbackTool{}
}

func (t *ScheduledCallbackTool) Name() string { return ScheduledCallbackToolName }

func (t *ScheduledCallbackTool) HiddenFromLLM() bool { return true }

func (t *ScheduledCallbackTool) Description() string {
	return "Calls a Tekshot scheduled publish callback URL when a cron job is due."
}

func (t *ScheduledCallbackTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"callback_url": map[string]any{
				"type":        "string",
				"description": "Absolute Drupal callback URL.",
			},
			"callback_token": map[string]any{
				"type":        "string",
				"description": "Opaque token sent to Drupal in X-Tekshot-Schedule-Token.",
			},
			"external_id": map[string]any{
				"type":        "string",
				"description": "Stable external id for debugging.",
			},
			"timeout_ms": map[string]any{
				"type":        "number",
				"description": "HTTP timeout in milliseconds.",
			},
		},
		"required": []string{"callback_url", "callback_token"},
	}
}

func (t *ScheduledCallbackTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	callbackURL := scheduledCallbackStringArg(args, "callback_url")
	callbackToken := scheduledCallbackStringArg(args, "callback_token")
	externalID := scheduledCallbackStringArg(args, "external_id")
	if callbackURL == "" {
		return tools.ErrorResult("callback_url is required")
	}
	if callbackToken == "" {
		return tools.ErrorResult("callback_token is required")
	}
	parsed, err := url.Parse(callbackURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return tools.ErrorResult("callback_url must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return tools.ErrorResult("callback_url must use http or https")
	}

	timeout := time.Duration(scheduledCallbackNumberArg(args, "timeout_ms", 30000)) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 2*time.Minute {
		timeout = 2 * time.Minute
	}

	body, _ := json.Marshal(map[string]any{
		"ok":          true,
		"source":      "goclaw_scheduled_callback",
		"external_id": externalID,
	})

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("create callback request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tekshot-Schedule-Token", callbackToken)

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("callback request failed: %v", err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(respBody))
		if message == "" {
			message = resp.Status
		}
		return tools.ErrorResult(fmt.Sprintf("callback returned HTTP %d: %s", resp.StatusCode, message))
	}

	return tools.NewResult(fmt.Sprintf("scheduled callback delivered: %s", externalID))
}

func scheduledCallbackStringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func scheduledCallbackNumberArg(args map[string]any, key string, fallback int64) int64 {
	switch value := args[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return parsed
		}
	}
	return fallback
}
