package tools

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeCronStore struct {
	addedSchedule store.CronSchedule
	addErr        error
}

func (f *fakeCronStore) AddJob(ctx context.Context, name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	f.addedSchedule = schedule
	if f.addErr != nil {
		return nil, f.addErr
	}
	return &store.CronJob{ID: "job-1", Name: name, Schedule: schedule, Payload: store.CronPayload{Message: message}}, nil
}
func (f *fakeCronStore) GetJob(ctx context.Context, jobID string) (*store.CronJob, bool) {
	return nil, false
}
func (f *fakeCronStore) ListJobs(ctx context.Context, includeDisabled bool, agentID, userID string) []store.CronJob {
	return nil
}
func (f *fakeCronStore) RemoveJob(ctx context.Context, jobID string) error { return nil }
func (f *fakeCronStore) UpdateJob(ctx context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	return nil, nil
}
func (f *fakeCronStore) EnableJob(ctx context.Context, jobID string, enabled bool) error { return nil }
func (f *fakeCronStore) GetRunLog(ctx context.Context, jobID string, limit, offset int) ([]store.CronRunLogEntry, int) {
	return nil, 0
}
func (f *fakeCronStore) Status() map[string]any { return map[string]any{} }
func (f *fakeCronStore) Start() error           { return nil }
func (f *fakeCronStore) Stop()                  {}
func (f *fakeCronStore) SetOnJob(handler func(job *store.CronJob) (*store.CronJobResult, error)) {
}
func (f *fakeCronStore) SetOnEvent(handler func(event store.CronEvent)) {}
func (f *fakeCronStore) RunJob(ctx context.Context, jobID string, force bool) (bool, string, error) {
	return false, "", nil
}
func (f *fakeCronStore) SetDefaultTimezone(tz string)      {}
func (f *fakeCronStore) GetDueJobs(now time.Time) []store.CronJob { return nil }

func TestCronToolAddRandomWindow(t *testing.T) {
	fake := &fakeCronStore{}
	tool := NewCronTool(fake)
	result := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name":     "facebook-morning",
			"schedule": map[string]any{"kind": "random_window", "expr": "0 9 * * 1,3,5", "tz": "Asia/Ho_Chi_Minh", "windowMs": float64(7_200_000)},
			"message":  "Post to Facebook",
		},
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if fake.addedSchedule.Kind != "random_window" {
		t.Fatalf("got kind %q", fake.addedSchedule.Kind)
	}
	if fake.addedSchedule.WindowMS == nil || *fake.addedSchedule.WindowMS != 7_200_000 {
		t.Fatalf("windowMs not preserved: %#v", fake.addedSchedule.WindowMS)
	}
}

func TestCronToolRejectsRandomWindowWithoutWindow(t *testing.T) {
	tool := NewCronTool(&fakeCronStore{})
	result := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name":     "facebook-morning",
			"schedule": map[string]any{"kind": "random_window", "expr": "0 9 * * 1,3,5"},
			"message":  "Post to Facebook",
		},
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
}
