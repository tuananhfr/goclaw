//go:build sqlite || sqliteonly

package sqlitestore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestTracingAggregatesPreserveStudioJobMetadata(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	ctx := sqliteTenantCtx(tenantID)
	s := NewSQLiteTracingStore(db)
	jobID := uuid.NewString()
	metadata, err := json.Marshal(map[string]any{"job_type": "studio_image", "tekshot_job_id": jobID})
	if err != nil {
		t.Fatal(err)
	}
	trace := &store.TraceData{ID: uuid.New(), AgentID: &agentID, Status: store.TraceStatusRunning, StartTime: time.Now().UTC(), CreatedAt: time.Now().UTC(), Metadata: metadata}
	if err := s.CreateTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSpan(ctx, &store.SpanData{
		ID: uuid.New(), TraceID: trace.ID, AgentID: &agentID, TenantID: tenantID, SpanType: store.SpanTypeLLMCall,
		Status: store.SpanStatusCompleted, StartTime: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		InputTokens: 41, OutputTokens: 13, Metadata: json.RawMessage(`{"cache_read_tokens":7}`),
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.BatchUpdateTraceAggregates(ctx, trace.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetTrace(ctx, trace.ID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got.Metadata, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["tekshot_job_id"] != jobID || decoded["job_type"] != "studio_image" || decoded["total_cache_read_tokens"] != float64(7) {
		t.Fatalf("metadata after aggregation = %s", got.Metadata)
	}
	if got.TotalInputTokens != 41 || got.TotalOutputTokens != 13 || got.LLMCallCount != 1 {
		t.Fatalf("trace aggregates = %+v", got)
	}
}
