package tekshot

import (
	"strings"
	"testing"
)

func plan(entries ...checklistPlanEntry) []checklistPlanEntry { return entries }

func TestRepetitionFlagsTheSameHookTypeOnTwoConsecutiveRows(t *testing.T) {
	findings := checklistRepetitionFindings(plan(
		checklistPlanEntry{Label: "a", Date: "2026-10-01", Topic: "Bài một", HookType: "CAU_HOI"},
		checklistPlanEntry{Label: "b", Date: "2026-10-03", Topic: "Bài hai", HookType: "CAU_HOI"},
		checklistPlanEntry{Label: "c", Date: "2026-10-05", Topic: "Bài ba", HookType: "CON_SO"},
	), nil)

	if len(findings[0]) != 0 || len(findings[2]) != 0 {
		t.Fatalf("only the later row of a pair may be flagged, got %v", findings)
	}
	if len(findings[1]) != 1 || !strings.Contains(findings[1][0], "kiểu hook CAU_HOI") {
		t.Fatalf("expected a hook finding on row b, got %v", findings[1])
	}
}

func TestRepetitionHookLooksAtTheLatestHistoryEntryToo(t *testing.T) {
	findings := checklistRepetitionFindings(
		plan(checklistPlanEntry{Date: "2026-10-01", Topic: "Bài mới", HookType: "CON_SO"}),
		[]checklistHistoryEntry{{Date: "2026-09-29", Topic: "Bài đã duyệt", HookType: "CON_SO"}},
	)
	if len(findings[0]) != 1 {
		t.Fatalf("expected the hook to clash with the previous approved row, got %v", findings[0])
	}
}

func TestRepetitionStoryWindowIsFourteenDays(t *testing.T) {
	history := []checklistHistoryEntry{{Date: "2026-10-01", Topic: "Chuyện khách cũ", StoryType: "CHUYEN_KHACH"}}

	inside := checklistRepetitionFindings(plan(checklistPlanEntry{Date: "2026-10-14", Topic: "Bài mới", StoryType: "CHUYEN_KHACH"}), history)
	if len(inside[0]) != 1 || !strings.Contains(inside[0][0], "13 ngày") {
		t.Fatalf("expected a story finding at 13 days, got %v", inside[0])
	}
	outside := checklistRepetitionFindings(plan(checklistPlanEntry{Date: "2026-10-15", Topic: "Bài mới", StoryType: "CHUYEN_KHACH"}), history)
	if len(outside[0]) != 0 {
		t.Fatalf("14 days apart is allowed, got %v", outside[0])
	}
}

func TestRepetitionTopicOverlapNeedsEnoughWords(t *testing.T) {
	long := "3 số kiểm tra khi quán đông nhưng doanh thu chưa như kỳ vọng"
	near := "3 số kiểm tra khi quán đông mà doanh thu chưa tăng"
	findings := checklistRepetitionFindings(
		plan(checklistPlanEntry{Date: "2026-10-01", Topic: near}),
		[]checklistHistoryEntry{{Date: "2026-09-10", Topic: long, Source: "posted"}},
	)
	if len(findings[0]) != 1 || !strings.Contains(findings[0][0], "chủ đề trùng") {
		t.Fatalf("expected a topic overlap finding, got %v", findings[0])
	}

	short := checklistRepetitionFindings(
		plan(checklistPlanEntry{Date: "2026-10-01", Topic: "Combo trưa nay"}),
		[]checklistHistoryEntry{{Date: "2026-09-10", Topic: "Combo trưa nay"}},
	)
	if len(short[0]) != 0 {
		t.Fatalf("a three-word title is too short to compare, got %v", short[0])
	}
}

func TestRepetitionTopicOverlapIgnoresUnicodeNormalisation(t *testing.T) {
	composed := "Khung 7 số quản lý chuỗi nhỏ nên xem mỗi ngày"
	decomposed := "Khung 7 số quản lý chuỗi nhỏ nên xem mỗi ngày"
	ratio, ok := topicOverlap(composed, decomposed)
	if !ok || ratio < 0.9 {
		t.Fatalf("same title in two Unicode forms must match, got %v %v", ratio, ok)
	}
}

func TestRepetitionSkipsTheRowBeingReplaced(t *testing.T) {
	topic := "3 số kiểm tra khi quán đông nhưng doanh thu chưa như kỳ vọng"
	findings := checklistRepetitionFindings(
		plan(checklistPlanEntry{ID: 7, Date: "2026-10-01", Topic: topic}),
		[]checklistHistoryEntry{{ID: 7, Date: "2026-10-01", Topic: topic}},
	)
	if len(findings[0]) != 0 {
		t.Fatalf("a row must not repeat itself, got %v", findings[0])
	}
}

func TestEnforceRepetitionSendsBackTwiceThenKeepsWithAWarning(t *testing.T) {
	history := []checklistHistoryEntry{{Date: "2026-09-30", Topic: "Bài trước", HookType: "CAU_HOI"}}
	newItems := func() []any {
		return []any{map[string]any{"date": "2026-10-01", "topic": "Bài mới", "kieu_hook": "CAU_HOI"}}
	}
	rejects := 0

	for attempt := 1; attempt <= checklistMaxRepeatRewrites; attempt++ {
		err := enforceChecklistRepetition(newItems(), history, nil, &rejects)
		if err == nil || !strings.Contains(err.Error(), "REPETITION") {
			t.Fatalf("attempt %d: expected the plan to be sent back, got %v", attempt, err)
		}
	}

	items := newItems()
	if err := enforceChecklistRepetition(items, history, nil, &rejects); err != nil {
		t.Fatalf("after %d rewrites the row must be accepted, got %v", checklistMaxRepeatRewrites, err)
	}
	warnings, _ := items[0].(map[string]any)["canh_bao_lap"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("expected the accepted row to carry its warning, got %v", warnings)
	}
}

func TestEnforceRepetitionLeavesCleanRowsAlone(t *testing.T) {
	items := []any{map[string]any{"date": "2026-10-01", "topic": "Bài mới", "kieu_hook": "CON_SO"}}
	rejects := 0
	if err := enforceChecklistRepetition(items, nil, nil, &rejects); err != nil || rejects != 0 {
		t.Fatalf("a clean plan must pass untouched, got %v (rejects %d)", err, rejects)
	}
	if _, has := items[0].(map[string]any)["canh_bao_lap"]; has {
		t.Fatal("a clean row must carry no warning key")
	}
}

func TestEnforceRepetitionIgnoresKeepAndDeleteRows(t *testing.T) {
	history := []checklistHistoryEntry{{ID: 9, Date: "2026-09-30", Topic: "Bài bị xoá", HookType: "CAU_HOI"}}
	items := []any{
		map[string]any{"action": "delete", "source_item_id": float64(9), "date": "2026-09-30", "topic": "Bài bị xoá"},
		map[string]any{"action": "keep", "source_item_id": float64(4), "date": "2026-10-01", "topic": "Bài giữ", "kieu_hook": "CAU_HOI"},
		map[string]any{"action": "create", "source_item_id": float64(0), "date": "2026-10-02", "topic": "Bài mới", "kieu_hook": "CAU_HOI"},
	}
	rejects := 0
	if err := enforceChecklistRepetition(items, history, nil, &rejects); err != nil {
		t.Fatalf("a deleted row must not count as history and keep rows are not checked: %v", err)
	}
}

func TestChecklistHistoryFromRequestSkipsEntriesWithoutATopic(t *testing.T) {
	history := checklistHistoryFromRequest(map[string]any{"history": []any{
		map[string]any{"id": float64(3), "date": "2026-09-01", "topic": "Có chủ đề", "hook_type": "con_so", "source": "checklist"},
		map[string]any{"date": "2026-09-02", "topic": "  "},
	}})
	if len(history) != 1 || history[0].ID != 3 || history[0].HookType != "CON_SO" {
		t.Fatalf("unexpected history %+v", history)
	}
}
