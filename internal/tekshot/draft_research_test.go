package tekshot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestValidateFactSheetAcceptsFactsWithSources(t *testing.T) {
	sheet, err := validateFactSheet(map[string]any{
		"facts": []any{
			map[string]any{"fact": "Ủ trà ô long 15 phút với 100 ml nước sôi", "source": "https://vidu.example.com/tra-sua"},
			map[string]any{"fact": "Hộ kinh doanh bỏ thuế khoán từ 01/01/2026", "source": "vault:thue-2026.md"},
		},
		"gaps": []any{"Số ly thành phẩm"},
	})
	if err != nil {
		t.Fatalf("valid sheet rejected: %v", err)
	}
	if len(sheet.Facts) != 2 || sheet.Facts[0].Source != "https://vidu.example.com/tra-sua" {
		t.Fatalf("facts parsed wrong: %+v", sheet.Facts)
	}
	if len(sheet.Gaps) != 1 || sheet.Gaps[0] != "Số ly thành phẩm" {
		t.Fatalf("gaps parsed wrong: %+v", sheet.Gaps)
	}
}

func TestValidateFactSheetAcceptsAnEmptySheet(t *testing.T) {
	// Nguồn đã đủ thì bảng rỗng là câu trả lời đúng, không phải lỗi.
	sheet, err := validateFactSheet(map[string]any{"facts": []any{}})
	if err != nil || len(sheet.Facts) != 0 {
		t.Fatalf("empty sheet should be valid, got %+v / %v", sheet, err)
	}
}

func TestValidateFactSheetRejectsAFactWithoutSource(t *testing.T) {
	_, err := validateFactSheet(map[string]any{
		"facts": []any{map[string]any{"fact": "Nướng 180°C trong 20 phút", "source": "  "}},
	})
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("fact without source must be rejected, got %v", err)
	}
}

func TestValidateFactSheetTrimsInsteadOfRefusing(t *testing.T) {
	facts := make([]any, 0, factSheetMaxFacts+5)
	for i := range factSheetMaxFacts + 5 {
		facts = append(facts, map[string]any{"fact": fmt.Sprintf("dữ kiện %d", i), "source": "vault:a.md"})
	}
	sheet, err := validateFactSheet(map[string]any{"facts": facts})
	if err != nil || len(sheet.Facts) != factSheetMaxFacts {
		t.Fatalf("oversized sheet should be trimmed to %d, got %d / %v", factSheetMaxFacts, len(sheet.Facts), err)
	}
}

func TestFactSheetCollectorKeepsOnlyValidSubmissions(t *testing.T) {
	collector := NewFactSheetCollector()
	if result := collector.Execute(context.Background(), map[string]any{"facts": "not a list"}); !result.IsError {
		t.Fatalf("malformed sheet must be rejected")
	}
	if collector.Sheet() != nil {
		t.Fatalf("rejected submission must not be kept")
	}
	collector.Execute(context.Background(), map[string]any{"facts": []any{map[string]any{"fact": "A", "source": "vault:a.md"}}})
	if sheet := collector.Sheet(); sheet == nil || len(sheet.Facts) != 1 {
		t.Fatalf("valid submission not kept: %+v", sheet)
	}
}

func researchArgs() map[string]any {
	return map[string]any{
		"workspace": map[string]any{"label": "Quán Mẫu"},
		"source_items": []any{map[string]any{
			"source_index":   1,
			"checklist_item": "CÔNG THỨC TRÀ SỮA KHOAI MÔN",
			"source_title":   "CÔNG THỨC TRÀ SỮA KHOAI MÔN",
			"source_brief":   "Đậm vị - chuẩn kinh doanh",
			"source_text":    "Title: CÔNG THỨC TRÀ SỮA KHOAI MÔN\nBrief: Đậm vị - chuẩn kinh doanh\nUSP - Key words: Khoai môn tươi 200 gr",
		}},
	}
}

func TestDraftResearchRequestIsAResearchOnlyRun(t *testing.T) {
	req := draftResearchRequest(researchArgs(), "tekshot-drupal-user-1", "tekshot:draft:x", NewFactSheetCollector())

	if !req.LightContext || req.SessionKey != "tekshot:draft:x:research" {
		t.Fatalf("research run should be light and in its own session, got %q light=%v", req.SessionKey, req.LightContext)
	}
	for _, tool := range []string{"vault_search", "web_search", "web_fetch"} {
		if !slices.Contains(req.ToolAllow, tool) {
			t.Fatalf("research run must allow %s: %v", tool, req.ToolAllow)
		}
	}
	if len(req.EphemeralTools) != 1 || req.EphemeralTools[0].Name() != factSheetToolName {
		t.Fatalf("research run must carry the fact sheet collector")
	}
	if req.MaxIterations != draftResearchIterations {
		t.Fatalf("research iterations = %d", req.MaxIterations)
	}
	for _, want := range []string{"CÔNG THỨC TRÀ SỮA KHOAI MÔN", "Khoai môn tươi 200 gr", "chưa có", "gaps", "đơn vị", "Không viết bài", factSheetToolName} {
		if !strings.Contains(req.Message, want) {
			t.Fatalf("research prompt missing %q:\n%s", want, req.Message)
		}
	}
}

func TestDraftResearchPromptHasABudgetAndARecipeRule(t *testing.T) {
	prompt := buildDraftResearchPrompt(researchArgs())
	// Không ngân sách thì một lượt gọi 15-25 search/fetch và chạm trần 150s.
	for _, want := range []string{"tối đa 3 lần web_search", "3 lần web_fetch", "nộp ngay"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("research prompt missing budget %q:\n%s", want, prompt)
		}
	}
	// Trà sữa ra 0 dữ kiện ba lần liền: định lượng trên mạng khác quán nên
	// model bỏ luôn cả bước làm chung.
	for _, want := range []string{"bước làm chung", "định lượng của page", "luôn thắng"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("research prompt missing recipe rule %q:\n%s", want, prompt)
		}
	}
	if draftResearchIterations != 6 {
		t.Fatalf("research iterations = %d, want 6", draftResearchIterations)
	}
}

func TestDraftResearchPromptSurvivesADeadSearchProvider(t *testing.T) {
	prompt := buildDraftResearchPrompt(researchArgs())
	// DuckDuckGo chặn máy sau vài chục bài: mỗi lần gọi lại tốn 30s timeout,
	// và bước tra từng nộp bảng rỗng dù kỹ thuật cần viết là kiến thức phổ thông.
	for _, want := range []string{
		"kiến thức phổ thông",
		`"kiến thức chung"`,
		"bắt buộc có nguồn Vault hoặc web",
		"không gọi lại",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("research prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestValidateFactSheetAcceptsGeneralKnowledgeAsASource(t *testing.T) {
	sheet, err := validateFactSheet(map[string]any{
		"facts": []any{map[string]any{"fact": "Khoai môn gọt vỏ, hấp chín rồi nghiền mịn", "source": "kiến thức chung"}},
	})
	if err != nil || len(sheet.Facts) != 1 || sheet.Facts[0].Source != "kiến thức chung" {
		t.Fatalf("general knowledge must be an accepted source: %+v / %v", sheet, err)
	}
}

func TestRenderFactSheetHidesSources(t *testing.T) {
	block := renderFactSheet(&draftFactSheet{
		Facts: []draftFact{{Fact: "Ủ trà 15 phút", Source: "https://vidu.example.com/x"}},
		Gaps:  []string{"Số ly thành phẩm"},
	})
	// Người viết không cần biết nguồn; đưa nguồn vào prompt là mời nó in ra.
	if !strings.Contains(block, "Ủ trà 15 phút") || strings.Contains(block, "vidu.example.com") {
		t.Fatalf("fact block should carry facts without sources:\n%s", block)
	}
	if !strings.Contains(block, "Số ly thành phẩm") {
		t.Fatalf("fact block should list what research could not find:\n%s", block)
	}
	if renderFactSheet(nil) != "" || renderFactSheet(&draftFactSheet{}) != "" {
		t.Fatalf("an empty sheet must render nothing")
	}
}

func TestBuildPromptPlacesResearchedFactsAfterTheSourceRow(t *testing.T) {
	args := researchArgs()
	args["researched_facts"] = renderFactSheet(&draftFactSheet{Facts: []draftFact{{Fact: "Ủ trà 15 phút", Source: "vault:a.md"}}})
	prompt := buildPrompt(args, defaultTimezone)

	// Tìm đúng tiêu đề khối: câu dẫn ở đầu prompt cũng nhắc tên khối này.
	source := strings.Index(prompt, "SOURCE RECORDS:")
	facts := strings.Index(prompt, "RESEARCHED FACTS (already verified")
	if source < 0 || facts < source || !strings.Contains(prompt, "Ủ trà 15 phút") {
		t.Fatalf("researched facts should follow the source records:\n%s", prompt)
	}
}

func TestBuildPromptNoLongerAsksTheWriterToSearch(t *testing.T) {
	prompt := buildPrompt(researchArgs(), defaultTimezone)
	for _, unwanted := range []string{"search the Vault first", "Use web search/fetch", "research facts with the allowed tools"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("writer prompt still asks to search (%q):\n%s", unwanted, prompt)
		}
	}
}
