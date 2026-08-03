package tekshot

import (
	"maps"
	"strings"
	"testing"
)

func validChecklistItem() map[string]any {
	return map[string]any{
		"date":         "2026-08-03",
		"timeline":     "Tuần 1 - hâm nóng",
		"time_slot":    "19:00-20:00",
		"content_line": "Món chủ lực",
		"topic":        "Pizza 4 vị phô mai cho bữa tối cuối tuần",
		"hook":         "4 loại phô mai kéo sợi trong một miếng bánh",
		"body":         "Quay cận cảnh lúc cắt miếng đầu tiên, nhấn phần phô mai kéo sợi. Chốt bằng lời mời đặt bàn tối thứ Bảy.",
		"usp":          "phô mai kéo sợi, nướng lửa, giao nhanh",
	}
}

func validChecklistReport() map[string]any {
	return map[string]any{
		"items":   []any{validChecklistItem(), validChecklistItem()},
		"summary": "Kế hoạch xoay quanh 3 tuyến: món chủ lực, khách hàng thật và ưu đãi cuối tuần.",
	}
}

func TestValidateContentChecklistAcceptsValidReport(t *testing.T) {
	if _, err := validateContentChecklist(validChecklistReport()); err != nil {
		t.Fatalf("expected valid checklist to pass, got: %v", err)
	}
}

func TestValidateContentChecklistRejectsEmptyItems(t *testing.T) {
	report := validChecklistReport()
	report["items"] = []any{}
	if _, err := validateContentChecklist(report); err == nil {
		t.Fatal("expected empty items to be rejected")
	}
}

func TestValidateContentChecklistRequiresSummary(t *testing.T) {
	report := validChecklistReport()
	report["summary"] = "   "
	if _, err := validateContentChecklist(report); err == nil {
		t.Fatal("expected blank summary to be rejected")
	}
}

func TestValidateContentChecklistRequiresPlanningColumns(t *testing.T) {
	for _, field := range checklistRequiredFields {
		report := validChecklistReport()
		item := validChecklistItem()
		item[field] = "  "
		report["items"] = []any{item}
		_, err := validateContentChecklist(report)
		if err == nil {
			t.Fatalf("expected blank %s to be rejected", field)
		}
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("expected error to name %s, got: %v", field, err)
		}
	}
}

func TestValidateContentChecklistAllowsBlankOptionalColumns(t *testing.T) {
	// timeline and time_slot are coordination columns the team often leaves
	// empty; blocking on them would make honest plans fail validation.
	report := validChecklistReport()
	item := validChecklistItem()
	item["timeline"] = ""
	item["time_slot"] = ""
	report["items"] = []any{item}
	if _, err := validateContentChecklist(report); err != nil {
		t.Fatalf("expected blank optional columns to pass, got: %v", err)
	}
}

func TestValidateContentChecklistRejectsBadDate(t *testing.T) {
	for _, bad := range []string{"03/08/2026", "2026-8-3", "next monday"} {
		report := validChecklistReport()
		item := validChecklistItem()
		item["date"] = bad
		report["items"] = []any{item}
		if _, err := validateContentChecklist(report); err == nil {
			t.Fatalf("expected date %q to be rejected", bad)
		}
	}
}

func TestValidateContentChecklistRejectsTooManyRows(t *testing.T) {
	report := validChecklistReport()
	rows := make([]any, 0, checklistMaxItems+1)
	for i := 0; i <= checklistMaxItems; i++ {
		rows = append(rows, validChecklistItem())
	}
	report["items"] = rows
	if _, err := validateContentChecklist(report); err == nil {
		t.Fatal("expected row count above the cap to be rejected")
	}
}

func TestBuildContentChecklistPromptBranchesOnPosCapability(t *testing.T) {
	base := map[string]any{
		"industry":   "Pizza / đồ ăn nhanh",
		"store_name": "Pizza Hip'S Đại Áng",
		"plan_from":  "2026-08-01",
		"plan_to":    "2026-08-07",
		"item_count": float64(7),
	}

	withPos := map[string]any{"has_pos": true}
	maps.Copy(withPos, base)
	prompt := buildContentChecklistPrompt(withPos)
	if !strings.Contains(prompt, "Sales numbers above are real") {
		t.Fatal("expected POS store prompt to assert the numbers are real")
	}

	withoutPos := map[string]any{"has_pos": false}
	maps.Copy(withoutPos, base)
	prompt = buildContentChecklistPrompt(withoutPos)
	if !strings.Contains(prompt, "NO sales data") {
		t.Fatal("expected non-POS store prompt to forbid inventing sales data")
	}
	if !strings.Contains(prompt, "(not available for this store)") {
		t.Fatal("expected the POS fact block to be marked unavailable")
	}
}

func TestBuildContentChecklistPromptWithPageNameIncludesPageContextAndRule(t *testing.T) {
	request := map[string]any{
		"industry":   "Pizza / đồ ăn nhanh",
		"store_name": "Pizza Hip'S",
		"page_name":  "Pizza Hip'S Đại Áng",
		"plan_from":  "2026-08-01",
		"plan_to":    "2026-08-07",
		"item_count": float64(7),
		"has_pos":    true,
	}

	prompt := buildContentChecklistPrompt(request)

	if !strings.Contains(prompt, "- Page: Pizza Hip'S Đại Áng\n") {
		t.Fatal("expected prompt to include the page context line")
	}
	if !strings.Contains(prompt, `This plan is for the page "Pizza Hip'S Đại Áng" specifically`) {
		t.Fatal("expected prompt to include the page-specific hard rule")
	}
	storeIdx := strings.Index(prompt, "- Store:")
	pageIdx := strings.Index(prompt, "- Page:")
	if storeIdx == -1 || pageIdx == -1 || pageIdx < storeIdx {
		t.Fatal("expected the page line to come right after the store line")
	}
}

func TestBuildContentChecklistPromptWithoutPageNameOmitsPageContextAndRule(t *testing.T) {
	base := map[string]any{
		"industry":   "Pizza / đồ ăn nhanh",
		"store_name": "Pizza Hip'S",
		"plan_from":  "2026-08-01",
		"plan_to":    "2026-08-07",
		"item_count": float64(7),
		"has_pos":    true,
	}

	// Absent entirely.
	prompt := buildContentChecklistPrompt(base)
	if strings.Contains(prompt, "- Page:") {
		t.Fatal("expected no page line when page_name is absent")
	}
	if strings.Contains(prompt, "specifically. Fit that page's own audience") {
		t.Fatal("expected no page-specific hard rule when page_name is absent")
	}

	// Present but blank.
	withBlank := map[string]any{"page_name": "   "}
	maps.Copy(withBlank, base)
	prompt = buildContentChecklistPrompt(withBlank)
	if strings.Contains(prompt, "- Page:") {
		t.Fatal("expected no page line when page_name is blank")
	}
	if strings.Contains(prompt, "specifically. Fit that page's own audience") {
		t.Fatal("expected no page-specific hard rule when page_name is blank")
	}

	// The rest of the prompt must still be well-formed: this is the
	// regression guard that matters, since every existing caller in
	// production today sends no page_name until Drupal is deployed.
	if !strings.Contains(prompt, "- Store: Pizza Hip'S\n") {
		t.Fatal("expected the store line to still be present")
	}
	if !strings.Contains(prompt, "- Industry: Pizza / đồ ăn nhanh\n") {
		t.Fatal("expected the industry line to still be present")
	}
	if !strings.Contains(prompt, "## Hard rules\n") {
		t.Fatal("expected the hard rules section to still be present")
	}
	if !strings.Contains(prompt, "Sales numbers above are real") {
		t.Fatal("expected the has_pos branch to still render correctly")
	}
	if !strings.Contains(prompt, checklistFinalToolName) {
		t.Fatal("expected the submission instruction to still reference the collector tool")
	}
}
