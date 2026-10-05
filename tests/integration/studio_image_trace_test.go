//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

func TestStudioImageTraceCollectorPersistsLifecycleAndMetadata(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	ts := pg.NewPGTracingStore(db)
	collector := tracing.NewCollector(ts)
	collector.Start()
	var stopOnce bool
	t.Cleanup(func() {
		if !stopOnce {
			collector.Stop()
		}
	})
	jobID := uuid.NewString()
	metadata, err := json.Marshal(map[string]any{"job_type": "studio_image", "tekshot_job_id": jobID})
	if err != nil {
		t.Fatal(err)
	}
	trace := &store.TraceData{ID: uuid.New(), AgentID: &agentID, UserID: "tekshot-42", SessionKey: "tekshot:image:42", Name: "tekshot studio image", Channel: "tekshot_job", Status: store.TraceStatusRunning, StartTime: time.Now().UTC(), CreatedAt: time.Now().UTC(), Metadata: metadata, Tags: []string{"tekshot", "studio_image"}}
	if err := collector.CreateTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	spanID := uuid.New()
	collector.EmitSpan(store.SpanData{ID: spanID, TraceID: trace.ID, AgentID: &agentID, TenantID: tenantID, SpanType: store.SpanTypeLLMCall, Name: "Draw image", Status: store.SpanStatusRunning, StartTime: time.Now().UTC(), CreatedAt: time.Now().UTC(), InputPreview: `{"prompt":"Draw a poster","images":[{"mime_type":"image/png","encoded_bytes":100}]}`})
	collector.EmitSpanUpdate(spanID, trace.ID, map[string]any{"status": store.SpanStatusCompleted, "end_time": time.Now().UTC(), "input_tokens": 41, "output_tokens": 13, "output_preview": "Image ready", "metadata": json.RawMessage(`{"cache_read_tokens":7}`)})
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	collector.FinishTrace(cancelledCtx, trace.ID, store.TraceStatusCompleted, "", "Saved poster.png")
	collector.Stop()
	stopOnce = true
	got, err := ts.GetTrace(ctx, trace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TraceStatusCompleted || got.EndTime == nil || got.LLMCallCount != 1 || got.TotalInputTokens != 41 || got.TotalOutputTokens != 13 {
		t.Fatalf("persisted trace = %+v", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got.Metadata, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["tekshot_job_id"] != jobID || decoded["job_type"] != "studio_image" || decoded["total_cache_read_tokens"] != float64(7) {
		t.Fatalf("metadata = %s", got.Metadata)
	}
	spans, err := ts.GetTraceSpans(ctx, trace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].Status != store.SpanStatusCompleted || !strings.Contains(spans[0].InputPreview, "Draw a poster") {
		t.Fatalf("persisted spans = %+v", spans)
	}
	traces, err := ts.ListTraces(ctx, store.TraceListOpts{AgentID: &agentID, UserID: "tekshot-42", SessionKey: trace.SessionKey, Limit: 10})
	if err != nil || len(traces) != 1 {
		t.Fatalf("agent-filtered traces=%d error=%v", len(traces), err)
	}
	otherCtx := store.WithTenantID(context.Background(), uuid.New())
	if other, err := ts.GetTrace(otherCtx, trace.ID); err == nil || other != nil {
		t.Fatal("trace visible across tenants")
	}
}
