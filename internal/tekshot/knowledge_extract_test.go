package tekshot

import (
	"fmt"
	"strings"
	"testing"
)

func validWebsitePages() []any {
	return []any{
		map[string]any{
			"url":      "https://example.com/bang-gia",
			"title":    "Ví Dụ – Bảng giá tháng 8",
			"summary":  "Giá bán từng món trong tháng 8.",
			"keywords": []any{"bảng giá", "cà phê", "25.000"},
			"markdown": "# Bảng giá\n\n| Món | Giá |\n|---|---|\n| Cà phê | 25.000 |",
		},
	}
}

func validKnowledgeArgs() map[string]any {
	return map[string]any{
		"title":    "Bảng giá tháng 8",
		"markdown": "# Bảng giá tháng 8\n\n| Món | Giá |\n|---|---|\n| Cà phê | 25.000 |",
		"language": "vi",
		"status":   "ok",
		"tool_health": map[string]any{
			"exec":   "ok",
			"vision": "unused",
		},
	}
}

func TestValidateKnowledgeExtractReportAcceptsValid(t *testing.T) {
	report, err := validateKnowledgeExtractReport(validKnowledgeArgs(), false)
	if err != nil {
		t.Fatalf("expected valid report, got error: %v", err)
	}
	if report["title"] != "Bảng giá tháng 8" {
		t.Fatalf("title lost in validation: %v", report["title"])
	}
}

func TestValidateKnowledgeExtractReportRejectsMissingMarkdownWhenOk(t *testing.T) {
	args := validKnowledgeArgs()
	args["markdown"] = "   "
	if _, err := validateKnowledgeExtractReport(args, false); err == nil {
		t.Fatal("expected error for empty markdown with status ok")
	}
}

func TestValidateKnowledgeExtractReportRejectsEmptyWithoutReason(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "empty"
	args["markdown"] = ""
	// không có reason → phải từ chối, agent phải giải thích
	if _, err := validateKnowledgeExtractReport(args, false); err == nil {
		t.Fatal("expected error for status empty without reason")
	}
}

func TestValidateKnowledgeExtractReportAcceptsEmptyWithReason(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "empty"
	args["markdown"] = ""
	args["reason"] = "Trang PDF trắng, không có nội dung nào để trích."
	if _, err := validateKnowledgeExtractReport(args, false); err != nil {
		t.Fatalf("expected empty+reason to be accepted, got: %v", err)
	}
}

func TestValidateKnowledgeExtractReportRejectsBadStatus(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "done"
	if _, err := validateKnowledgeExtractReport(args, false); err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestValidateKnowledgeExtractReportRejectsMissingToolHealth(t *testing.T) {
	args := validKnowledgeArgs()
	delete(args, "tool_health")
	if _, err := validateKnowledgeExtractReport(args, false); err == nil {
		t.Fatal("expected error for missing tool_health")
	}
}

func TestKnowledgeExtractPromptWebsiteUsesWebFetch(t *testing.T) {
	prompt := buildKnowledgeExtractPrompt(map[string]any{
		"website_url": "https://example.com/bai-viet",
	})
	if !strings.Contains(prompt, "web_fetch") {
		t.Fatal("website prompt must instruct web_fetch")
	}
}

func TestKnowledgeExtractPromptWebsiteCrawlsBeyondTheEntryPage(t *testing.T) {
	prompt := buildKnowledgeExtractPrompt(map[string]any{
		"website_url": "https://example.com/",
	})
	for _, want := range []string{"sitemap.xml", "12 content pages", "one entry in `pages`", "liên hệ", "phần 1/2", "total_pages_discovered", "NEVER web_fetch an attachment", "30 web_fetch calls TOTAL", "40000 characters"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("website prompt lost its crawl strategy, missing %q", want)
		}
	}
}

func TestValidateKnowledgeExtractReportWebsiteRequiresPages(t *testing.T) {
	args := validKnowledgeArgs()
	if _, err := validateKnowledgeExtractReport(args, true); err == nil {
		t.Fatal("expected error when a website extraction submits no pages")
	}
}

func TestValidateKnowledgeExtractReportWebsiteNormalizesPages(t *testing.T) {
	args := validKnowledgeArgs()
	dup := validWebsitePages()[0].(map[string]any)
	args["pages"] = append(validWebsitePages(), dup)
	report, err := validateKnowledgeExtractReport(args, true)
	if err != nil {
		t.Fatalf("expected valid website report, got: %v", err)
	}
	pages, ok := report["pages"].([]map[string]any)
	if !ok || len(pages) != 1 {
		t.Fatalf("pages must dedupe by url, got %v", report["pages"])
	}
	urls, ok := report["pages_fetched"].([]string)
	if !ok || len(urls) != 1 || urls[0] != "https://example.com/bang-gia" {
		t.Fatalf("pages_fetched must be derived from pages, got %v", report["pages_fetched"])
	}
}

func TestValidateKnowledgeExtractReportWebsitePageMissingTitleRejected(t *testing.T) {
	args := validKnowledgeArgs()
	page := validWebsitePages()[0].(map[string]any)
	page["title"] = "  "
	args["pages"] = []any{page}
	if _, err := validateKnowledgeExtractReport(args, true); err == nil {
		t.Fatal("expected error for a page without a title")
	}
}

func TestValidateKnowledgeExtractReportFileIgnoresPages(t *testing.T) {
	args := validKnowledgeArgs()
	args["pages"] = validWebsitePages()
	report, err := validateKnowledgeExtractReport(args, false)
	if err != nil {
		t.Fatalf("expected valid file report, got: %v", err)
	}
	if _, exists := report["pages"]; exists {
		t.Fatal("file sources must drop pages")
	}
	if _, exists := report["pages_fetched"]; exists {
		t.Fatal("file sources must drop pages_fetched")
	}
}

func TestValidateKnowledgeExtractReportKeywordsCappedAtEight(t *testing.T) {
	args := validKnowledgeArgs()
	page := validWebsitePages()[0].(map[string]any)
	kws := make([]any, 0, 12)
	for i := 0; i < 12; i++ {
		kws = append(kws, fmt.Sprintf("kw%d", i))
	}
	page["keywords"] = kws
	args["pages"] = []any{page}
	report, err := validateKnowledgeExtractReport(args, true)
	if err != nil {
		t.Fatalf("expected valid report, got: %v", err)
	}
	pages := report["pages"].([]map[string]any)
	if got := len(pages[0]["keywords"].([]string)); got != 8 {
		t.Fatalf("keywords must cap at 8, got %d", got)
	}
}
