package tekshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type studioImageTraceKey struct{}

type studioImageTrace struct {
	collector *tracing.Collector
	id        uuid.UUID
	agentID   *uuid.UUID
	token     string
	rawError  error
	output    any
}

func (s *JobService) startStudioImageTrace(ctx context.Context, job *store.TekshotJob) (context.Context, *studioImageTrace) {
	if job.JobType != TekshotJobTypeStudioImage || s.studio == nil || s.studio.TraceCollector == nil {
		return ctx, nil
	}
	ctx = store.WithTenantID(ctx, store.MasterTenantID)
	ctx = store.WithUserID(ctx, "tekshot-"+job.ExternalUserID)
	ctx = store.WithAgentKey(ctx, job.AgentKey)
	t := &studioImageTrace{collector: s.studio.TraceCollector, id: store.GenNewID(), token: job.CallbackToken}
	if resolved := studioImageAgentFromContext(ctx); resolved != nil && resolved.err == nil && resolved.data != nil && resolved.data.ID != uuid.Nil {
		t.agentID = &resolved.data.ID
	}
	now := time.Now().UTC()
	metadata, err := json.Marshal(map[string]any{
		"job_type":          job.JobType,
		"tekshot_job_id":    job.ID.String(),
		"external_job_uuid": job.ExternalJobUUID,
		"agent_key":         job.AgentKey,
	})
	if err != nil {
		slog.Warn("tekshot.studio_image.trace_metadata_failed", "job_id", job.ID, "error", err)
		return ctx, nil
	}
	request := map[string]any{}
	if err := json.Unmarshal(job.RequestJSON, &request); err != nil {
		request = nil
	}
	trace := &store.TraceData{
		ID:           t.id,
		AgentID:      t.agentID,
		UserID:       "tekshot-" + job.ExternalUserID,
		SessionKey:   job.SessionKey,
		RunID:        job.ID.String(),
		Name:         "tekshot studio image",
		Channel:      "tekshot_job",
		InputPreview: t.preview(stringFromMap(request, "message")),
		Status:       store.TraceStatusRunning,
		StartTime:    now,
		CreatedAt:    now,
		Tags:         []string{"tekshot", TekshotJobTypeStudioImage},
		Metadata:     metadata,
	}
	if trace.InputPreview == "" {
		trace.InputPreview = t.preview(stringFromMap(request, "prompt"))
	}
	createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := t.collector.CreateTrace(createCtx, trace); err != nil {
		slog.Warn("tekshot.studio_image.trace_create_failed", "job_id", job.ID, "error", err)
		return ctx, nil
	}
	ctx = context.WithValue(ctx, studioImageTraceKey{}, t)
	ctx = tracing.WithTraceID(ctx, t.id)
	ctx = tracing.WithCollector(ctx, t.collector)
	t.collector.SetTraceStatus(ctx, t.id, store.TraceStatusRunning)
	ctx = providers.WithStudioImageObservation(ctx, &providers.StudioImageObservation{
		Start: t.startNativeCall,
		Retry: t.recordRetry,
	})
	return ctx, t
}

func (t *studioImageTrace) finish(ctx context.Context, err error) {
	status := store.TraceStatusCompleted
	message := ""
	if err != nil {
		status = store.TraceStatusError
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			status = store.TraceStatusCancelled
		}
		if t.rawError != nil {
			err = t.rawError
		}
		message = t.preview(err.Error())
	}
	t.collector.FinishTrace(ctx, t.id, status, message, t.preview(t.output))
}

func studioImageTraceFromContext(ctx context.Context) *studioImageTrace {
	t, _ := ctx.Value(studioImageTraceKey{}).(*studioImageTrace)
	return t
}

func startStudioImageSpan(ctx context.Context, span store.SpanData, input any) (context.Context, func(any, error, *providers.Usage)) {
	t := studioImageTraceFromContext(ctx)
	if t == nil {
		return ctx, func(any, error, *providers.Usage) {}
	}
	now := time.Now().UTC()
	span.ID = store.GenNewID()
	span.TraceID = t.id
	span.AgentID = t.agentID
	span.TenantID = store.MasterTenantID
	span.StartTime = now
	span.CreatedAt = now
	span.Status = store.SpanStatusRunning
	span.Level = store.SpanLevelDefault
	span.InputPreview = t.preview(input)
	if parent := tracing.ParentSpanIDFromContext(ctx); parent != uuid.Nil {
		span.ParentSpanID = &parent
	}
	t.collector.EmitSpan(span)
	spanCtx := tracing.WithParentSpanID(ctx, span.ID)
	return spanCtx, func(output any, err error, usage *providers.Usage) {
		end := time.Now().UTC()
		updates := map[string]any{
			"end_time":       end,
			"duration_ms":    int(end.Sub(now).Milliseconds()),
			"status":         store.SpanStatusCompleted,
			"output_preview": t.preview(output),
		}
		if err != nil {
			updates["status"] = store.SpanStatusError
			updates["error"] = t.preview(err.Error())
		}
		if usage != nil {
			updates["input_tokens"] = usage.PromptTokens
			updates["output_tokens"] = usage.CompletionTokens
		}
		if observation := providers.ChatGPTOAuthRoutingObservationFromContext(spanCtx); observation != nil {
			evidence := observation.Snapshot()
			if evidence.HasData() {
				updates["metadata"] = providers.MergeChatGPTOAuthRoutingMetadata(span.Metadata, evidence)
				if span.SpanType == store.SpanTypeLLMCall && evidence.ServingProvider != "" {
					updates["provider"] = evidence.ServingProvider
				}
			}
		}
		t.collector.EmitSpanUpdate(span.ID, t.id, updates)
	}
}

func (t *studioImageTrace) startNativeCall(ctx context.Context, provider string, req providers.StudioImageRequest) (context.Context, func(*providers.StudioImageResult, error)) {
	// A native call belongs to one pool member; outer routing evidence belongs to its parent.
	ctx = providers.WithChatGPTOAuthRoutingObservation(ctx, nil)
	params := map[string]any{"image_model": req.ImageModel, "quality": req.Quality, "size": req.Size, "reasoning_effort": req.ReasoningEffort}
	if req.Size == "" {
		params["size"] = "auto"
	}
	if req.ReasoningEffort == "" {
		params["reasoning_effort"] = "low"
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		slog.Warn("tekshot.studio_image.trace_params_failed", "error", err)
	}
	images := make([]map[string]any, 0, len(req.Images))
	for i, image := range req.Images {
		images = append(images, map[string]any{"position": i + 1, "mime_type": image.MimeType, "encoded_bytes": len(image.Data)})
	}
	ctx, finish := startStudioImageSpan(ctx, store.SpanData{
		Name: fmt.Sprintf("Draw image: %s/%s", provider, req.Model), SpanType: store.SpanTypeLLMCall,
		Provider: provider, Model: req.Model, ModelParams: encoded,
	}, map[string]any{"instructions": req.Instructions, "prompt": req.Text, "parameters": params, "images": images})
	return ctx, func(result *providers.StudioImageResult, callErr error) {
		var usage *providers.Usage
		if result != nil {
			usage = result.Usage
		}
		finish(studioImageResultSummary(result), callErr, usage)
	}
}

func studioImageResultSummary(result *providers.StudioImageResult) any {
	if result == nil {
		return nil
	}
	return map[string]any{"content": result.Text, "revised_prompt": result.RevisedPrompt, "action": result.Action, "mime_type": result.MimeType, "image_bytes": len(result.Data)}
}

func (t *studioImageTrace) recordRetry(ctx context.Context, provider string, attempt, maxAttempts int, err error) {
	_, finish := startStudioImageSpan(ctx, store.SpanData{Name: "Retry model HTTP request", SpanType: store.SpanTypeEvent, Provider: provider},
		map[string]any{"failed_attempt": attempt, "max_attempts": maxAttempts})
	finish("Retrying after upstream error", err, nil)
}

func (t *studioImageTrace) preview(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			slog.Warn("tekshot.studio_image.trace_preview_failed", "error", err)
			return ""
		}
		text = string(encoded)
	}
	if t.token != "" {
		text = strings.ReplaceAll(text, t.token, "[redacted]")
	}
	return tracing.TruncateMid(text, t.collector.PreviewMaxLen())
}
