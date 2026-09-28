package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantScopedCronStore mirrors PGCronStore: inserts default to the master
// tenant, but every read and mutation is scoped by the tenant in ctx.
type tenantScopedCronStore struct {
	store.CronStore
	jobs map[string]store.CronJob
}

func newTenantScopedCronStore() *tenantScopedCronStore {
	return &tenantScopedCronStore{jobs: map[string]store.CronJob{}}
}

func (f *tenantScopedCronStore) scoped(ctx context.Context) bool {
	return store.TenantIDFromContext(ctx) != uuid.Nil
}

func (f *tenantScopedCronStore) AddToolCallJob(ctx context.Context, name string, atMS int64, toolName string, args map[string]any, agentID, userID string) (*store.CronJob, error) {
	id := uuid.NewString()
	f.jobs[id] = store.CronJob{
		ID: id, TenantID: store.MasterTenantID, Name: name, UserID: userID, Enabled: true,
		Schedule: store.CronSchedule{Kind: "at", AtMS: &atMS},
		Payload:  store.CronPayload{Kind: "tool_call", ToolName: toolName, Args: args},
	}
	job, _ := f.GetJob(ctx, id)
	return job, nil
}

func (f *tenantScopedCronStore) GetJob(ctx context.Context, jobID string) (*store.CronJob, bool) {
	job, ok := f.jobs[jobID]
	if !ok || !f.scoped(ctx) {
		return nil, false
	}
	return &job, true
}

func (f *tenantScopedCronStore) ListJobs(ctx context.Context, _ bool, _, _ string) []store.CronJob {
	if !f.scoped(ctx) {
		return nil
	}
	out := make([]store.CronJob, 0, len(f.jobs))
	for _, job := range f.jobs {
		out = append(out, job)
	}
	return out
}

func (f *tenantScopedCronStore) UpdateJob(ctx context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	job, ok := f.jobs[jobID]
	if !ok || !f.scoped(ctx) {
		return nil, store.ErrCronJobNotFound
	}
	job.Name = patch.Name
	if patch.Schedule != nil {
		job.Schedule = *patch.Schedule
	}
	job.Payload.Args = patch.ToolArgs
	f.jobs[jobID] = job
	return &job, nil
}

func (f *tenantScopedCronStore) RemoveJob(ctx context.Context, jobID string) error {
	if _, ok := f.jobs[jobID]; !ok || !f.scoped(ctx) {
		return store.ErrCronJobNotFound
	}
	delete(f.jobs, jobID)
	return nil
}

func scheduledCallbackServer(cron store.CronStore) *Server {
	cfg := &config.Config{}
	cfg.Gateway.Token = "gateway-secret"
	s := &Server{cfg: cfg, clients: make(map[string]*Client)}
	s.SetTekshotCronStore(cron)
	return s
}

// serveScheduled turns a handler panic into a test failure instead of aborting the package run.
func serveScheduled(t *testing.T, handler http.HandlerFunc, method, path, body string) (rec *httptest.ResponseRecorder) {
	t.Helper()
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer gateway-secret")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s %s panicked: %v", method, path, r)
		}
	}()
	handler(rec, req)
	return rec
}

func scheduledCallbackBody(externalID string) string {
	return fmt.Sprintf(`{"external_id":%q,"run_at_ms":%d,"callback_url":"https://drupal.example/callback","callback_token":"tok","method":"POST","timeout_ms":5000}`,
		externalID, time.Now().Add(time.Hour).UnixMilli())
}

func TestTekshotScheduledCallbackCreateWithoutTenantHeader(t *testing.T) {
	cron := newTenantScopedCronStore()
	s := scheduledCallbackServer(cron)

	rec := serveScheduled(t, s.handleTekshotScheduledCallbackJobs, http.MethodPost, "/v1/tekshot/scheduled-callback-jobs", scheduledCallbackBody("messenger-reply-1"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(cron.jobs) != 1 {
		t.Fatalf("expected one scheduled job, got %d", len(cron.jobs))
	}
}

func TestTekshotScheduledCallbackRescheduleReusesTheJob(t *testing.T) {
	cron := newTenantScopedCronStore()
	s := scheduledCallbackServer(cron)
	for range 2 {
		rec := serveScheduled(t, s.handleTekshotScheduledCallbackJobs, http.MethodPost, "/v1/tekshot/scheduled-callback-jobs", scheduledCallbackBody("messenger-reply-1"))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	if len(cron.jobs) != 1 {
		t.Fatalf("rescheduling the same external id must keep one job, got %d", len(cron.jobs))
	}
}

func TestTekshotScheduledCallbackDeleteRemovesTheJob(t *testing.T) {
	cron := newTenantScopedCronStore()
	s := scheduledCallbackServer(cron)
	serveScheduled(t, s.handleTekshotScheduledCallbackJobs, http.MethodPost, "/v1/tekshot/scheduled-callback-jobs", scheduledCallbackBody("memory-1"))
	var id string
	for key := range cron.jobs {
		id = key
	}
	rec := serveScheduled(t, s.handleTekshotScheduledCallbackJob, http.MethodDelete, "/v1/tekshot/scheduled-callback-jobs/"+id, "")
	if rec.Code != http.StatusOK || len(cron.jobs) != 0 {
		t.Fatalf("delete status = %d, jobs left = %d", rec.Code, len(cron.jobs))
	}
}

func TestTekshotScheduledCallbackStoreReturningNoJobIsAnError(t *testing.T) {
	s := scheduledCallbackServer(nilJobCronStore{})
	rec := serveScheduled(t, s.handleTekshotScheduledCallbackJobs, http.MethodPost, "/v1/tekshot/scheduled-callback-jobs", scheduledCallbackBody("seed-1"))
	if rec.Code == http.StatusAccepted {
		t.Fatalf("a job the store could not read back must not be reported as scheduled: %s", rec.Body.String())
	}
}

type nilJobCronStore struct{ store.CronStore }

func (nilJobCronStore) ListJobs(context.Context, bool, string, string) []store.CronJob { return nil }
func (nilJobCronStore) AddToolCallJob(context.Context, string, int64, string, map[string]any, string, string) (*store.CronJob, error) {
	return nil, nil
}
