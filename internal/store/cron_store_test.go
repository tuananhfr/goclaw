package store

import (
	"errors"
	"testing"
	"time"
)

func TestNextRunForToggle_DisableClearsNextRun(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, false, true, new(now.Add(time.Minute)), now, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != nil {
		t.Fatalf("expected disable toggle to clear next_run_at, got %v", next)
	}
}

func TestNextRunForToggle_EnableRecomputesEverySchedule(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected enable toggle to recompute next_run_at")
	}

	want := now.Add(time.Minute)
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}
}

func TestNextRunForToggle_EnableUsesDefaultTimezoneForCronSchedule(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind: "cron",
		Expr: "0 9 * * *",
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "America/Toronto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected enable toggle to compute next_run_at for cron schedule")
	}

	want := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}
}

func TestValidateCronSchedule_RandomWindow(t *testing.T) {
	window := int64(2 * time.Hour / time.Millisecond)
	valid := &CronSchedule{
		Kind:     "random_window",
		Expr:     "0 9 * * 1,3,5",
		TZ:       "Asia/Ho_Chi_Minh",
		WindowMS: &window,
	}
	if err := ValidateCronSchedule(valid); err != nil {
		t.Fatalf("valid random_window rejected: %v", err)
	}

	for name, schedule := range map[string]*CronSchedule{
		"missing_window": {Kind: "random_window", Expr: "0 9 * * *"},
		"zero_window":    {Kind: "random_window", Expr: "0 9 * * *", WindowMS: int64Ptr(0)},
		"bad_cron":       {Kind: "random_window", Expr: "bad cron", WindowMS: &window},
		"bad_timezone":   {Kind: "random_window", Expr: "0 9 * * *", TZ: "Invalid/Zone", WindowMS: &window},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCronSchedule(schedule); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestComputeNextRun_RandomWindowRangeAndTimezone(t *testing.T) {
	window := int64(2 * time.Hour / time.Millisecond)
	now := time.Date(2026, time.May, 15, 1, 0, 0, 0, time.UTC) // Friday 08:00 in Vietnam.
	schedule := &CronSchedule{
		Kind:     "random_window",
		Expr:     "0 9 * * 1,3,5",
		TZ:       "Asia/Ho_Chi_Minh",
		WindowMS: &window,
	}

	next := ComputeNextRun(schedule, now, "")
	if next == nil {
		t.Fatal("expected next random_window run")
	}
	windowStart := time.Date(2026, time.May, 15, 2, 0, 0, 0, time.UTC) // 09:00 +07.
	windowEnd := windowStart.Add(2 * time.Hour)
	if next.Before(windowStart) || !next.Before(windowEnd) {
		t.Fatalf("next run %v outside [%v, %v)", next, windowStart, windowEnd)
	}
}

func TestComputeNextRun_RandomWindowNextMatchingDay(t *testing.T) {
	window := int64(time.Hour / time.Millisecond)
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC) // after Friday window.
	schedule := &CronSchedule{
		Kind:     "random_window",
		Expr:     "0 9 * * 1,3,5",
		TZ:       "Asia/Ho_Chi_Minh",
		WindowMS: &window,
	}

	next := ComputeNextRun(schedule, now, "")
	if next == nil {
		t.Fatal("expected next random_window run")
	}
	windowStart := time.Date(2026, time.May, 18, 2, 0, 0, 0, time.UTC) // next Monday 09:00 +07.
	windowEnd := windowStart.Add(time.Hour)
	if next.Before(windowStart) || !next.Before(windowEnd) {
		t.Fatalf("next run %v outside [%v, %v)", next, windowStart, windowEnd)
	}
}

func TestNextRunForToggle_AlreadyEnabledPreservesCurrentNextRun(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	currentNextRun := now.Add(5 * time.Minute)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, true, true, &currentNextRun, now.Add(time.Minute), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected preserved next run")
	}
	if !next.Equal(currentNextRun) {
		t.Fatalf("got %v, want %v", next, currentNextRun)
	}
}

func TestNextRunForToggle_ExpiredAtReturnsError(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute).UnixMilli()
	schedule := &CronSchedule{
		Kind: "at",
		AtMS: &past,
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "")
	if next != nil {
		t.Fatalf("expected nil next run, got %v", next)
	}
	if err == nil {
		t.Fatal("expected error for expired at schedule")
	}
	if !errors.Is(err, ErrCronJobNoFutureRun) {
		t.Fatalf("got %v, want ErrCronJobNoFutureRun", err)
	}
}

//go:fix inline
func int64Ptr(v int64) *int64 {
	return &v
}

//go:fix inline
func timePtr(v time.Time) *time.Time {
	return &v
}
