package tekshot

import (
	"strings"
	"testing"
)

func checklistGuideTestRequest() map[string]any {
	return map[string]any{
		"page_name": "ERPcons",
		"table": map[string]any{
			"name":    "ERPcons",
			"headers": []any{"NGÀY THÁNG", "TUYẾN NỘI DUNG", "CHỦ ĐỀ BÀI VIẾT", "NHẬN XÉT"},
			"rows": []any{
				[]any{"04/06/2026", "Product Demo", "Quản lý không cần có mặt tại công trình", "Duyệt"},
				[]any{"", "  ", "", ""},
				"not a row",
				[]any{"06/06/2026", "Tin tức", "Chuyển đổi số\nngành vật liệu", ""},
			},
		},
	}
}

func TestChecklistGuideTableSkipsEmptyAndMalformedRows(t *testing.T) {
	table := checklistGuideTableFromRequest(checklistGuideTestRequest())
	if table.Name != "ERPcons" || len(table.Headers) != 4 {
		t.Fatalf("table parsed wrong: %+v", table)
	}
	if len(table.Rows) != 2 {
		t.Fatalf("expected 2 usable rows, got %d", len(table.Rows))
	}
	if table.Rows[1][2] != "Chuyển đổi số ngành vật liệu" {
		t.Fatalf("cell must be folded onto one line, got %q", table.Rows[1][2])
	}
}

func TestChecklistGuideTableCapsRowsAndCells(t *testing.T) {
	rows := make([]any, 0, checklistGuideMaxRows+5)
	for i := 0; i < checklistGuideMaxRows+5; i++ {
		rows = append(rows, []any{strings.Repeat("ờ", checklistGuideCellMaxRunes+40)})
	}
	table := checklistGuideTableFromRequest(map[string]any{"table": map[string]any{"rows": rows}})
	if len(table.Rows) != checklistGuideMaxRows {
		t.Fatalf("expected %d rows, got %d", checklistGuideMaxRows, len(table.Rows))
	}
	if got := len([]rune(table.Rows[0][0])); got != checklistGuideCellMaxRunes+1 {
		t.Fatalf("expected the cell cut to %d runes plus an ellipsis, got %d", checklistGuideCellMaxRunes, got)
	}
}

func TestBuildChecklistGuidePrompt(t *testing.T) {
	request := checklistGuideTestRequest()
	prompt := buildChecklistGuidePrompt(request, checklistGuideTableFromRequest(request), 1500)

	for _, want := range []string{
		"- Name: ERPcons",
		"## The team's plan table \"ERPcons\" (2 rows)",
		"- TUYẾN NỘI DUNG: Product Demo",
		"- NHẬN XÉT: Duyệt",
		"\"Người duyệt hay nhắc\"",
		"Never list, quote or paraphrase an individual topic",
		"about 1200 characters and never more than 1500",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected the prompt to contain %q", want)
		}
	}
	// Ô trống không được thành một dòng "NHẬN XÉT:" rỗng.
	if strings.Contains(prompt, "- NHẬN XÉT: \n") {
		t.Fatal("an empty cell must not be printed")
	}
}

func TestCleanChecklistGuideDropsCodeFence(t *testing.T) {
	if got := cleanChecklistGuide("```markdown\nTuyến nội dung\n- Ba tuyến\n```\n"); got != "Tuyến nội dung\n- Ba tuyến" {
		t.Fatalf("unexpected cleaned note: %q", got)
	}
	if got := cleanChecklistGuide("  Nhịp đăng: 3 bài mỗi tuần  "); got != "Nhịp đăng: 3 bài mỗi tuần" {
		t.Fatalf("a plain note must only be trimmed, got %q", got)
	}
}

func TestChecklistGuideFromRequestCapsLength(t *testing.T) {
	if got := checklistGuideFromRequest(map[string]any{}); got != "" {
		t.Fatalf("a page without a note must read as empty, got %q", got)
	}
	long := strings.Repeat("a", checklistGuidePromptMaxRunes+10)
	if got := len([]rune(checklistGuideFromRequest(map[string]any{"checklist_guide": long}))); got != checklistGuidePromptMaxRunes {
		t.Fatalf("expected the note cut to %d runes, got %d", checklistGuidePromptMaxRunes, got)
	}
}

func TestPlanningPromptsCarryTheGuideAsReferenceOnly(t *testing.T) {
	const guide = "Tuyến nội dung: Case Study, Founder POV."
	const heading = "## How this page has planned its posts so far (reference only)"

	for name, build := range map[string]func(map[string]any) string{
		"plan": buildContentChecklistPrompt,
		"chat": buildContentChecklistChatPrompt,
	} {
		with := build(map[string]any{"checklist_guide": guide})
		for _, want := range []string{heading, guide, "It is a reference, not a rule and not a topic list"} {
			if !strings.Contains(with, want) {
				t.Fatalf("%s prompt must contain %q when the page has a note", name, want)
			}
		}
		// Page chưa học thì prompt phải y như trước khi có tính năng này.
		if without := build(map[string]any{}); strings.Contains(without, heading) {
			t.Fatalf("%s prompt must not mention planning habits when the page has no note", name)
		}
	}
}
