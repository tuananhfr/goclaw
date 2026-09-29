package tekshot

import (
	"strings"
	"testing"
)

func planFrameWith(formats map[string]any, purposes []any, audiences []any) checklistPlanFrame {
	return checklistPlanFrameFromRequest(map[string]any{
		"plan_frame": map[string]any{"formats": formats, "purposes": purposes, "audiences": audiences},
	})
}

func TestChecklistPlanFieldsAcceptAValidRowAndNormaliseCase(t *testing.T) {
	item := validChecklistItem()
	item["giai_doan"] = "hanh_dong"
	item["cta_chinh"] = "dat_hang"
	item["rui_ro"] = "low"
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err != nil {
		t.Fatalf("expected valid row, got %v", err)
	}
	if item["giai_doan"] != "HANH_DONG" || item["cta_chinh"] != "DAT_HANG" || item["rui_ro"] != "LOW" {
		t.Fatalf("expected codes upper-cased, got %v", item)
	}
}

func TestChecklistPlanFieldsRejectASalesCTAAtAwareness(t *testing.T) {
	item := validChecklistItem()
	item["giai_doan"] = "NHAN_BIET"
	item["cta_chinh"] = "DAT_HANG"
	err := validateChecklistPlanFields(item, testPlanFrame(), 2)
	if err == nil || !strings.Contains(err.Error(), "items[2].cta_chinh") {
		t.Fatalf("expected awareness row with an order CTA to be refused, got %v", err)
	}
}

func TestChecklistPlanFieldsRejectASalesCTAOnAnInformationPost(t *testing.T) {
	item := validChecklistItem()
	item["muc_dich"] = "THONG_TIN"
	item["cta_chinh"] = "HOTLINE"
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err == nil {
		t.Fatal("expected an information post with a hotline CTA to be refused")
	}
}

func TestChecklistPlanFieldsNeedAKeywordOnlyForCommentKeywordCTA(t *testing.T) {
	item := validChecklistItem()
	item["giai_doan"] = "CAN_NHAC"
	item["cta_chinh"] = "COMMENT_TU_KHOA"
	item["tu_khoa_cta"] = ""
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err == nil {
		t.Fatal("expected a missing comment keyword to be refused")
	}

	item["tu_khoa_cta"] = "bảng giá"
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err != nil {
		t.Fatalf("expected keyword row to pass, got %v", err)
	}
	if item["tu_khoa_cta"] != "BẢNG GIÁ" {
		t.Fatalf("expected keyword upper-cased, got %q", item["tu_khoa_cta"])
	}

	other := validChecklistItem()
	other["tu_khoa_cta"] = "STRAY"
	if err := validateChecklistPlanFields(other, testPlanFrame(), 0); err != nil {
		t.Fatalf("expected stray keyword to be cleared, not refused: %v", err)
	}
	if other["tu_khoa_cta"] != "" {
		t.Fatalf("expected keyword cleared for a non-keyword CTA, got %q", other["tu_khoa_cta"])
	}
}

func TestChecklistPlanFieldsHoldTheAudienceToTheBusinessProfile(t *testing.T) {
	frame := planFrameWith(nil, nil, []any{"Gia đình trẻ", "Dân văn phòng"})
	item := validChecklistItem()
	item["tep_khach"] = "Sinh viên"
	if err := validateChecklistPlanFields(item, frame, 0); err == nil {
		t.Fatal("expected an audience outside the profile to be refused")
	}

	item["tep_khach"] = "dân văn phòng"
	if err := validateChecklistPlanFields(item, frame, 0); err != nil {
		t.Fatalf("expected case-insensitive audience match, got %v", err)
	}
	if item["tep_khach"] != "Dân văn phòng" {
		t.Fatalf("expected the profile's spelling, got %q", item["tep_khach"])
	}
}

func TestChecklistPlanFieldsHoldFormatAndPurposeToThePageProfile(t *testing.T) {
	frame := planFrameWith(map[string]any{"F1": "Cẩm nang", "F7": "Thông báo vận hành"}, []any{"THONG_TIN"}, nil)
	item := validChecklistItem()
	item["cta_chinh"] = "THEO_DOI"
	item["dinh_dang"] = "F2"
	item["muc_dich"] = "THONG_TIN"
	if err := validateChecklistPlanFields(item, frame, 0); err == nil || !strings.Contains(err.Error(), "dinh_dang") {
		t.Fatalf("expected a format outside the page profile to be refused, got %v", err)
	}

	item["dinh_dang"] = "F1"
	item["muc_dich"] = "THUONG_MAI"
	if err := validateChecklistPlanFields(item, frame, 0); err == nil || !strings.Contains(err.Error(), "muc_dich") {
		t.Fatalf("expected a purpose outside the page profile to be refused, got %v", err)
	}
}

func TestChecklistPlanFieldsReadAnUnknownRiskAsHigh(t *testing.T) {
	item := validChecklistItem()
	item["rui_ro"] = "unsure"
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item["rui_ro"] != riskHigh {
		t.Fatalf("expected unknown risk to read as HIGH, got %q", item["rui_ro"])
	}
}

func TestChecklistPlanFieldsDropTheStyleOfAnUploadedPhoto(t *testing.T) {
	item := validChecklistItem()
	item["loai_anh"] = "UPLOAD"
	item["style_anh"] = "POSTER"
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item["style_anh"] != "" {
		t.Fatalf("expected style cleared for UPLOAD, got %q", item["style_anh"])
	}
}

func TestChecklistChatKeepRowsNeedNoPlanColumns(t *testing.T) {
	keep := map[string]any{
		"action": "keep", "source_item_id": float64(7), "reason": "Giữ nguyên",
		"date": "2026-08-03", "time_slot": "", "content_line": "Món chủ lực", "topic": "Pizza",
		"hook": "Hook", "body": "Nội dung: A. Ảnh: B.", "usp": "phô mai",
	}
	report := map[string]any{
		"type": "proposal", "reply": "Đề xuất.", "summary": "Một dòng.",
		"items": []any{keep}, "sources": []any{}, "research_status": map[string]any{},
	}
	if _, err := validateContentChecklistProposal(report, testPlanFrame()); err != nil {
		t.Fatalf("expected a keep row from before the plan columns to pass, got %v", err)
	}
}

func TestContentChecklistPromptListsThePagesFormats(t *testing.T) {
	prompt := buildContentChecklistPrompt(map[string]any{
		"store_name": "Pizza Hip'S",
		"plan_frame": map[string]any{
			"formats":   map[string]any{"F10": "Hướng dẫn sử dụng", "F2": "Giới thiệu sản phẩm"},
			"audiences": []any{"Gia đình trẻ"},
		},
	})
	for _, expected := range []string{"## Planning columns (Content Master)", "  - F2: Giới thiệu sản phẩm\n  - F10:", "Copy exactly one of: Gia đình trẻ"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}
}

func TestWriterPromptFollowsPlanColumnsOnlyWhenTheRowHasThem(t *testing.T) {
	withPlan := []SourceItem{{SourceIndex: 1, SourceTitle: "Pizza", SourceText: "Title: Pizza\nMã bài: P1-261003-9\nCTA chính: Đặt hàng"}}
	if !sourceItemsCarryPlanColumns(withPlan) {
		t.Fatal("expected a pushed Insight row to switch the plan rules on")
	}
	plain := []SourceItem{{SourceIndex: 1, SourceTitle: "Pizza", SourceText: "Title: Pizza\nGhi chú: CTA mềm"}}
	if sourceItemsCarryPlanColumns(plain) {
		t.Fatal("expected a hand-imported sheet to keep the old prompt")
	}
}
