package tekshot

import (
	"strings"
	"testing"
)

func TestValidateContentChecklistRejectsVideoFormats(t *testing.T) {
	for _, directive := range []string{
		"Nội dung: Làm video dọc 20 giây giới thiệu trang. Ảnh: Không áp dụng.",
		"Nội dung: Quay một góc làm việc thật. Ảnh: Không áp dụng.",
		"Nội dung: Đăng reel chia sẻ kiến thức. Ảnh: Không áp dụng.",
	} {
		report := validChecklistReport()
		item := validChecklistItem()
		item["body"] = directive
		report["items"] = []any{item}
		if _, err := validateContentChecklist(report); err == nil {
			t.Fatalf("expected unsupported format to be rejected: %q", directive)
		}
	}
}

func TestBuildContentChecklistPromptSetsStaticImageContract(t *testing.T) {
	prompt := buildContentChecklistPrompt(map[string]any{
		"store_name":         "Nghề & Nghiệp",
		"page_name":          "Nghề & Nghiệp",
		"has_pos":            false,
		"editorial_contract": map[string]any{"post_format": "written Facebook post with one static image"},
	})

	for _, expected := range []string{
		"## Editorial format contract",
		"written Facebook post paired with a static image",
		"Reach and performance metrics are planning signals only",
		"do NOT put dashboard figures, weekly performance recaps",
		"unique-viewer metric is not evidence of new viewers",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}
}

func TestBuildContentChecklistChatPromptKeepsReachMetricsOutOfRows(t *testing.T) {
	prompt := buildContentChecklistChatPrompt(map[string]any{})
	for _, expected := range []string{
		"Reach and performance metrics are planning signals only",
		"do NOT put dashboard figures, weekly performance recaps",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected chat prompt to contain %q", expected)
		}
	}
}

func TestChecklistCollectorsDoNotAcceptTimelineFromTheModel(t *testing.T) {
	legacyItems := NewContentChecklistCollectorTool().Parameters()["properties"].(map[string]any)["items"].(map[string]any)
	legacyItem := legacyItems["items"].(map[string]any)
	legacyFields := legacyItem["properties"].(map[string]any)
	if _, found := legacyFields["timeline"]; found {
		t.Fatal("legacy checklist collector must not accept an AI-supplied timeline")
	}

	chatItems := NewContentChecklistProposalCollector().Parameters()["properties"].(map[string]any)["items"].(map[string]any)
	chatItem := chatItems["items"].(map[string]any)
	chatFields := chatItem["properties"].(map[string]any)
	if _, found := chatFields["timeline"]; found {
		t.Fatal("checklist chat collector must not accept an AI-supplied timeline")
	}
}

func TestValidateContentChecklistProposalRejectsVideoFormats(t *testing.T) {
	item := validChecklistItem()
	item["action"] = "create"
	item["source_item_id"] = float64(0)
	item["reason"] = "Tạo bài mới"
	item["body"] = "Nội dung: Làm video giới thiệu trang. Ảnh: Không áp dụng."
	report := map[string]any{
		"type":            "proposal",
		"reply":           "Đã chuẩn bị đề xuất.",
		"summary":         "Một bài giới thiệu.",
		"items":           []any{item},
		"sources":         []any{},
		"research_status": map[string]any{},
	}

	if _, err := validateContentChecklistProposal(report); err == nil {
		t.Fatal("expected checklist chat proposal with video to be rejected")
	}
}
