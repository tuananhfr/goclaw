package tekshot

import (
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
	for k, v := range base {
		withPos[k] = v
	}
	prompt := buildContentChecklistPrompt(withPos)
	if !strings.Contains(prompt, "Sales numbers above are real") {
		t.Fatal("expected POS store prompt to assert the numbers are real")
	}

	withoutPos := map[string]any{"has_pos": false}
	for k, v := range base {
		withoutPos[k] = v
	}
	prompt = buildContentChecklistPrompt(withoutPos)
	if !strings.Contains(prompt, "NO sales data") {
		t.Fatal("expected non-POS store prompt to forbid inventing sales data")
	}
	if !strings.Contains(prompt, "(not available for this store)") {
		t.Fatal("expected the POS fact block to be marked unavailable")
	}
}
