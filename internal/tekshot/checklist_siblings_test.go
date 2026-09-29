package tekshot

import (
	"strings"
	"testing"
)

func erpconsPage(rows ...checklistSiblingRow) []checklistSiblingPage {
	return []checklistSiblingPage{{Name: "ERPcons", OwnedTopics: []string{"Phần mềm xây dựng"}, Rows: rows}}
}

func TestSiblingSlotsOnTheSameDayMustBeTwoHoursApart(t *testing.T) {
	pages := erpconsPage(checklistSiblingRow{Date: "2026-10-01", TimeSlot: "19:30", Topic: "Bài ERPcons"})

	close := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-01", TimeSlot: "19:00-20:00", Topic: "Bài Tekshot"}), pages)
	if len(close[0]) != 1 || !strings.Contains(close[0][0], "đăng 19:00 cùng ngày") || !strings.Contains(close[0][0], "19:30") {
		t.Fatalf("expected a slot clash 30 minutes apart, got %v", close[0])
	}

	apart := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-01", TimeSlot: "21h30", Topic: "Bài Tekshot"}), pages)
	if len(apart[0]) != 0 {
		t.Fatalf("two hours apart is fine, got %v", apart[0])
	}
	otherDay := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-02", TimeSlot: "19:30", Topic: "Bài Tekshot"}), pages)
	if len(otherDay[0]) != 0 {
		t.Fatalf("another day never clashes on time, got %v", otherDay[0])
	}
	vague := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-01", TimeSlot: "buổi tối", Topic: "Bài Tekshot"}), pages)
	if len(vague[0]) != 0 {
		t.Fatalf("an unreadable slot is skipped, not guessed, got %v", vague[0])
	}
}

func TestSiblingCTAKeywordMustNotBeShared(t *testing.T) {
	pages := erpconsPage(checklistSiblingRow{Date: "2026-10-20", Topic: "Bài ERPcons", Keyword: "AUDIT"})

	shared := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-08", Topic: "Bài Tekshot", Keyword: "audit"}), pages)
	if len(shared[0]) != 1 || !strings.Contains(shared[0][0], "từ khoá CTA «audit»") {
		t.Fatalf("expected a keyword clash within 30 days, got %v", shared[0])
	}
	far := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-08-01", Topic: "Bài Tekshot", Keyword: "AUDIT"}), pages)
	if len(far[0]) != 0 {
		t.Fatalf("80 days apart is outside the window, got %v", far[0])
	}
}

func TestSiblingTopicClashNeedsTheSameAudienceWithinAWeek(t *testing.T) {
	topic := "3 số kiểm tra khi quán đông nhưng doanh thu chưa như kỳ vọng"
	pages := erpconsPage(checklistSiblingRow{Date: "2026-10-01", Topic: topic, Audience: "Chủ quán F&B"})

	same := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-04", Topic: topic, Audience: "chủ quán f&b"}), pages)
	if len(same[0]) != 1 || !strings.Contains(same[0][0], "cách 3 ngày") {
		t.Fatalf("expected a topic clash for the same audience, got %v", same[0])
	}
	otherAudience := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-04", Topic: topic, Audience: "Nhà thầu"}), pages)
	if len(otherAudience[0]) != 0 {
		t.Fatalf("a different customer group may share the topic, got %v", otherAudience[0])
	}
	later := checklistSiblingFindings(plan(checklistPlanEntry{Date: "2026-10-12", Topic: topic, Audience: "Chủ quán F&B"}), pages)
	if len(later[0]) != 0 {
		t.Fatalf("11 days apart is outside the window, got %v", later[0])
	}
}

func TestSiblingChecksJoinTheRewriteLoop(t *testing.T) {
	pages := erpconsPage(checklistSiblingRow{Date: "2026-10-01", Topic: "Bài ERPcons", Keyword: "AUDIT"})
	items := func() []any {
		return []any{map[string]any{"date": "2026-10-02", "topic": "Bài Tekshot mới", "tu_khoa_cta": "AUDIT"}}
	}
	rejects := 0
	if err := enforceChecklistRepetition(items(), nil, pages, &rejects); err == nil || !strings.Contains(err.Error(), "từ khoá CTA") {
		t.Fatalf("a shared keyword must send the plan back, got %v", err)
	}
	rejects = checklistMaxRepeatRewrites
	kept := items()
	if err := enforceChecklistRepetition(kept, nil, pages, &rejects); err != nil {
		t.Fatalf("after the rewrites the row is kept: %v", err)
	}
	if warnings, _ := kept[0].(map[string]any)["canh_bao_lap"].([]any); len(warnings) != 1 {
		t.Fatalf("the kept row must carry the cross-page warning, got %v", warnings)
	}
}

func TestSiblingsFromRequestAndPrompt(t *testing.T) {
	request := map[string]any{"siblings": []any{
		map[string]any{"page": "ERPcons", "owned_topics": []any{"Phần mềm xây dựng"}, "rows": []any{
			map[string]any{"date": "2026-10-01", "time_slot": "19:30", "topic": "Bài ERPcons", "audience": "Nhà thầu", "keyword": "báo giá"},
			map[string]any{"date": "2026-10-02", "topic": " "},
		}},
		map[string]any{"page": "Trống", "rows": []any{}},
	}}

	pages := checklistSiblingsFromRequest(request)
	if len(pages) != 1 || len(pages[0].Rows) != 1 || pages[0].Rows[0].Keyword != "BÁO GIÁ" {
		t.Fatalf("unexpected siblings %+v", pages)
	}

	var sb strings.Builder
	writeChecklistSiblings(&sb, pages)
	for _, expected := range []string{"Other pages of the same owner", "Page \"ERPcons\" owns the topics: Phần mềm xây dựng", "2026-10-01 19:30 Bài ERPcons (audience Nhà thầu, keyword BÁO GIÁ)", "at least 2 hours apart"} {
		if !strings.Contains(sb.String(), expected) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", expected, sb.String())
		}
	}
}

func TestBusinessProfilePromptListsOwnedTopics(t *testing.T) {
	var sb strings.Builder
	profile := readBusinessProfile(map[string]any{"business_profile": map[string]any{
		"name": "Tekshot", "description": "Phần mềm quán", "owned_topics": []any{"Chuyển đổi số F&B", "Camera AI"},
	}})
	profile.writeProfile(&sb)
	if !strings.Contains(sb.String(), "Topic clusters this page owns (prefer them): Chuyển đổi số F&B; Camera AI") {
		t.Fatalf("expected owned topics in the profile block, got:\n%s", sb.String())
	}
}
