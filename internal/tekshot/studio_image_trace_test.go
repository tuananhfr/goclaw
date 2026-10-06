package tekshot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/providertest"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type imageTraceMemoryStore struct {
	store.TracingStore
	mu        sync.Mutex
	traces    map[uuid.UUID]store.TraceData
	spans     map[uuid.UUID]store.SpanData
	createErr error
}

func (s *imageTraceMemoryStore) CreateTrace(ctx context.Context, trace *store.TraceData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	if store.TenantIDFromContext(ctx) != store.MasterTenantID {
		return errors.New("trace tenant missing")
	}
	s.traces[trace.ID] = *trace
	return nil
}

func (s *imageTraceMemoryStore) UpdateTrace(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || store.TenantIDFromContext(ctx) != store.MasterTenantID {
		return errors.New("trace update requires detached tenant context")
	}
	trace := s.traces[id]
	if value, ok := updates["status"].(string); ok {
		trace.Status = value
	}
	if value, ok := updates["error"].(string); ok {
		trace.Error = value
	}
	if value, ok := updates["output_preview"].(string); ok {
		trace.OutputPreview = value
	}
	if value, ok := updates["end_time"].(time.Time); ok {
		trace.EndTime = &value
	}
	s.traces[id] = trace
	return nil
}

func (s *imageTraceMemoryStore) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, span := range spans {
		s.spans[span.ID] = span
	}
	return nil
}

func (s *imageTraceMemoryStore) UpdateSpan(_ context.Context, id uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	span := s.spans[id]
	if value, ok := updates["status"].(string); ok {
		span.Status = value
	}
	if value, ok := updates["error"].(string); ok {
		span.Error = value
	}
	if value, ok := updates["provider"].(string); ok {
		span.Provider = value
	}
	if value, ok := updates["output_preview"].(string); ok {
		span.OutputPreview = value
	}
	if value, ok := updates["end_time"].(time.Time); ok {
		span.EndTime = &value
	}
	if value, ok := updates["duration_ms"].(int); ok {
		span.DurationMS = value
	}
	if value, ok := updates["input_tokens"].(int); ok {
		span.InputTokens = value
	}
	if value, ok := updates["output_tokens"].(int); ok {
		span.OutputTokens = value
	}
	if value, ok := updates["metadata"].(json.RawMessage); ok {
		span.Metadata = value
	}
	s.spans[id] = span
	return nil
}

func (s *imageTraceMemoryStore) BatchUpdateTraceAggregates(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := s.traces[id]
	trace.SpanCount, trace.LLMCallCount, trace.TotalInputTokens, trace.TotalOutputTokens = 0, 0, 0, 0
	for _, span := range s.spans {
		if span.TraceID != id {
			continue
		}
		trace.SpanCount++
		if span.SpanType == store.SpanTypeLLMCall {
			trace.LLMCallCount++
			trace.TotalInputTokens += span.InputTokens
			trace.TotalOutputTokens += span.OutputTokens
		}
	}
	s.traces[id] = trace
	return nil
}

func (*imageTraceMemoryStore) DeleteTracesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type imageTraceJobStore struct {
	store.TekshotJobStore
	completeErr error
}

func (*imageTraceJobStore) MarkRunning(context.Context, uuid.UUID, string, time.Duration) error {
	return nil
}
func (s *imageTraceJobStore) MarkCompleted(context.Context, uuid.UUID, json.RawMessage, string) error {
	return s.completeErr
}

type imageTraceAgentStore struct {
	store.AgentCRUDStore
	id        uuid.UUID
	workspace string
	err       error
	calls     int
}

func (s *imageTraceAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &store.AgentData{BaseModel: store.BaseModel{ID: s.id}, AgentKey: key, Workspace: s.workspace}, nil
}

type imageTraceFixture struct {
	service   *JobService
	store     *imageTraceMemoryStore
	collector *tracing.Collector
	job       *store.TekshotJob
	stopOnce  sync.Once
}

func newImageTraceFixture(t *testing.T, s *JobService) *imageTraceFixture {
	t.Helper()
	f := &imageTraceFixture{service: s, store: &imageTraceMemoryStore{traces: map[uuid.UUID]store.TraceData{}, spans: map[uuid.UUID]store.SpanData{}}}
	f.collector = tracing.NewCollector(f.store)
	f.collector.Start()
	t.Cleanup(f.stop)
	s.studio.TraceCollector = f.collector
	s.studio.Agents = &imageTraceAgentStore{id: uuid.New(), workspace: filepath.Join(s.studio.Workspace, "image-agent")}
	s.store = &imageTraceJobStore{}
	f.job = &store.TekshotJob{ID: uuid.New(), JobType: TekshotJobTypeStudioImage, AgentKey: "image-agent", ExternalUserID: "42", ExternalJobUUID: uuid.NewString(), SessionKey: "tekshot:post:42:image", CallbackToken: "secret-callback-token"}
	return f
}

func (f *imageTraceFixture) stop() { f.stopOnce.Do(f.collector.Stop) }

func (f *imageTraceFixture) process(t *testing.T, ctx context.Context, args map[string]any) error {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	f.job.RequestJSON = encoded
	err = f.service.process(ctx, f.job)
	f.stop()
	return err
}

func (f *imageTraceFixture) snapshot(t *testing.T) (store.TraceData, []store.SpanData) {
	t.Helper()
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(f.store.traces))
	}
	var trace store.TraceData
	for _, value := range f.store.traces {
		trace = value
	}
	spans := make([]store.SpanData, 0, len(f.store.spans))
	for _, span := range f.store.spans {
		if span.Status == store.SpanStatusRunning || span.EndTime == nil {
			t.Fatalf("unfinished span %s", span.Name)
		}
		spans = append(spans, span)
	}
	return trace, spans
}

func imageTraceStream() string {
	return "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Image ready.\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"" + studioTestPNG + "\",\"output_format\":\"png\",\"revised_prompt\":\"A poster\",\"action\":\"generate\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":41,\"output_tokens\":13,\"total_tokens\":54}}}\n\ndata: [DONE]\n\n"
}

func imageTraceServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestStudioImageTraceSuccess(t *testing.T) {
	server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
	s, _ := newStudioService(t, server, map[string]string{"poster": "Use large readable lettering."})
	f := newImageTraceFixture(t, s)
	args := studioDrawArgs()
	args["tagged_skills"] = []any{"poster"}
	args["media"] = []any{map[string]any{"path": writeTempPNG(t), "role": "base"}}
	args["callback_token"] = f.job.CallbackToken
	args["authorization"] = "secret-provider-token"
	if err := f.process(t, context.Background(), args); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if trace.Status != store.TraceStatusCompleted || trace.EndTime == nil || trace.UserID != "tekshot-42" || trace.AgentID == nil || trace.SessionKey != f.job.SessionKey {
		t.Fatalf("trace = %+v", trace)
	}
	if trace.LLMCallCount != 1 || trace.TotalInputTokens != 41 || trace.TotalOutputTokens != 13 {
		t.Fatalf("wrong usage: %+v", trace)
	}
	foundPrompt := false
	for _, span := range spans {
		if span.SpanType == store.SpanTypeLLMCall {
			foundPrompt = strings.Contains(span.InputPreview, "Use large readable lettering.") && strings.Contains(span.InputPreview, "instructions") && strings.Contains(span.OutputPreview, "A poster")
		}
	}
	if !foundPrompt {
		t.Fatal("native trace did not include effective input and revised prompt")
	}
	encoded, err := json.Marshal(struct {
		Trace store.TraceData
		Spans []store.SpanData
	}{trace, spans})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{studioTestPNG, f.job.CallbackToken, "secret-provider-token"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("trace leaked %q", secret)
		}
	}
}

func TestStudioImageTraceProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"bad_request", 400, "invalid request", "HTTP 400"},
		{"rate_limit", 429, "rate limited", "HTTP 429"},
		{"overload", 503, "upstream busy", "HTTP 503"},
		{"refusal", 200, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Cannot draw this.\"}\n\ndata: [DONE]\n", "Cannot draw this."},
		{"stream_failure", 200, "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\ndata: [DONE]\n", "stream failed"},
		{"bad_image", 200, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"!invalid\"}}\n\ndata: [DONE]\n", "decode image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			s, _ := newStudioService(t, server, nil)
			f := newImageTraceFixture(t, s)
			if err := f.process(t, context.Background(), studioDrawArgs()); err == nil {
				t.Fatal("want failure")
			}
			trace, spans := f.snapshot(t)
			if trace.Status != store.TraceStatusError || !strings.Contains(trace.Error, tc.want) {
				t.Fatalf("trace error = %q, want %q", trace.Error, tc.want)
			}
			found := false
			for _, span := range spans {
				if span.SpanType == store.SpanTypeLLMCall && span.Status == store.SpanStatusError && strings.Contains(span.Error, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatal("provider failure missing from native span")
			}
		})
	}
}

func TestStudioImageTraceHTTPRetryDoesNotDoubleUsage(t *testing.T) {
	var hits atomic.Int32
	server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "retry-me", 503)
			return
		}
		_, _ = io.WriteString(w, imageTraceStream())
	})
	s, _ := newStudioService(t, server, nil)
	s.studio.Providers.Register(providertest.NewCodexProviderFast("openai-codex", server.URL).WithRetryConfig(providers.RetryConfig{Attempts: 2, MinDelay: time.Millisecond, MaxDelay: time.Millisecond}))
	f := newImageTraceFixture(t, s)
	if err := f.process(t, context.Background(), studioDrawArgs()); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if hits.Load() != 2 || trace.LLMCallCount != 1 || trace.TotalInputTokens != 41 {
		t.Fatalf("hits=%d trace=%+v", hits.Load(), trace)
	}
	found := false
	for _, span := range spans {
		if span.Name == "Retry model HTTP request" && strings.Contains(span.Error, "retry-me") {
			found = true
		}
	}
	if !found {
		t.Fatal("HTTP retry missing")
	}
}

func TestStudioImageTraceCancellationAndDeadline(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		name := "deadline"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			server := imageTraceServer(t, func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
			})
			s, _ := newStudioService(t, server, nil)
			f := newImageTraceFixture(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if cancelled {
				go func() { <-started; cancel() }()
			}
			if err := f.process(t, ctx, studioDrawArgs()); err == nil {
				t.Fatal("want interruption")
			}
			trace, _ := f.snapshot(t)
			want := store.TraceStatusError
			if cancelled {
				want = store.TraceStatusCancelled
			}
			if trace.Status != want || trace.EndTime == nil {
				t.Fatalf("trace = %+v, want %s", trace, want)
			}
		})
	}
}

func TestStudioImageTraceLocalFailures(t *testing.T) {
	for _, tc := range []string{"empty_prompt", "bad_media", "save_image", "save_job", "malformed_json"} {
		t.Run(tc, func(t *testing.T) {
			server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
			s, _ := newStudioService(t, server, nil)
			f := newImageTraceFixture(t, s)
			args := studioDrawArgs()
			switch tc {
			case "empty_prompt":
				args = map[string]any{}
			case "bad_media":
				args["media"] = []any{map[string]any{"path": filepath.Join(t.TempDir(), "missing.png")}}
			case "save_image":
				path := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
				s.studio.Workspace = path
			case "save_job":
				s.store = &imageTraceJobStore{completeErr: errors.New("database write failed")}
			}
			var err error
			if tc == "malformed_json" {
				f.job.RequestJSON = []byte("{")
				err = s.process(context.Background(), f.job)
				f.stop()
			} else {
				err = f.process(t, context.Background(), args)
			}
			if err == nil {
				t.Fatal("want failure")
			}
			trace, _ := f.snapshot(t)
			if trace.Status != store.TraceStatusError || trace.Error == "" {
				t.Fatalf("trace = %+v", trace)
			}
		})
	}
}

func TestStudioImageTraceReferenceFailureCanRecover(t *testing.T) {
	var bodies []map[string]any
	server := studioCodexServer(t, "unreadable choice", &bodies)
	s, _ := newStudioService(t, server, nil)
	f := newImageTraceFixture(t, s)
	args := studioDrawArgs()
	args["reference_library"] = []any{map[string]any{"id": 12, "url": server.URL + "/ref.png", "description": "Poster reference"}}
	if err := f.process(t, context.Background(), args); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if trace.Status != store.TraceStatusCompleted || trace.LLMCallCount != 3 {
		t.Fatalf("trace = %+v", trace)
	}
	failedParses := 0
	for _, span := range spans {
		if span.Name == "Parse reference selection" && span.Status == store.SpanStatusError {
			failedParses++
		}
	}
	if failedParses != 2 {
		t.Fatalf("failed parses = %d, want 2", failedParses)
	}
}

func TestStudioImageTraceUnavailableDoesNotBreakDrawing(t *testing.T) {
	server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
	s, _ := newStudioService(t, server, nil)
	f := newImageTraceFixture(t, s)
	f.store.createErr = errors.New("trace database unavailable")
	if err := f.process(t, context.Background(), studioDrawArgs()); err != nil {
		t.Fatal(err)
	}
	if len(f.store.traces) != 0 || len(f.store.spans) != 0 {
		t.Fatal("orphan trace spans emitted")
	}
}

func TestStudioImageTraceProviderFailover(t *testing.T) {
	failed := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "first provider busy", 503) })
	succeeded := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
	s, _ := newStudioService(t, failed, nil)
	s.studio.Providers.Register(providertest.NewCodexProviderFast("fallback-codex", succeeded.URL))
	s.studio.BuiltinTools = fakeBuiltinTools{settings: json.RawMessage(`{"providers":[{"provider":"openai-codex","model":"gpt-5.6-luna","enabled":true},{"provider":"fallback-codex","model":"gpt-5.6-luna","enabled":true}]}`)}
	f := newImageTraceFixture(t, s)
	if err := f.process(t, context.Background(), studioDrawArgs()); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if trace.Status != store.TraceStatusCompleted || trace.LLMCallCount != 2 || trace.TotalInputTokens != 41 {
		t.Fatalf("trace = %+v", trace)
	}
	failedCall, successfulCall := false, false
	for _, span := range spans {
		if span.SpanType != store.SpanTypeLLMCall {
			continue
		}
		failedCall = failedCall || span.Provider == "openai-codex" && span.Status == store.SpanStatusError && strings.Contains(span.Error, "first provider busy")
		successfulCall = successfulCall || span.Provider == "fallback-codex" && span.Status == store.SpanStatusCompleted
	}
	if !failedCall || !successfulCall {
		t.Fatalf("failover evidence missing: %+v", spans)
	}
}

func TestStudioImageTraceReferenceDownloadFailureCanRecover(t *testing.T) {
	var bodies []map[string]any
	server := studioCodexServer(t, `{"id":12}`, &bodies)
	failedDownload := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "missing reference", 404) })
	s, _ := newStudioService(t, server, nil)
	f := newImageTraceFixture(t, s)
	args := studioDrawArgs()
	args["reference_library"] = []any{map[string]any{"id": 12, "url": failedDownload.URL + "/ref.png", "description": "Poster reference"}}
	args["tagged_skills"] = []any{"missing-skill"}
	if err := f.process(t, context.Background(), args); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if trace.Status != store.TraceStatusCompleted || trace.LLMCallCount != 2 {
		t.Fatalf("trace = %+v", trace)
	}
	failedDownloadLogged, missingSkillLogged := false, false
	for _, span := range spans {
		failedDownloadLogged = failedDownloadLogged || span.Name == "Download reference image" && strings.Contains(span.Error, "HTTP 404")
		missingSkillLogged = missingSkillLogged || span.Name == "Load tagged image skill" && strings.Contains(span.Error, "missing-skill")
	}
	if !failedDownloadLogged || !missingSkillLogged {
		t.Fatalf("recoverable failures missing: %+v", spans)
	}
}
