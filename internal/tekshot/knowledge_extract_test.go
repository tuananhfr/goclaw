package tekshot

import (
	"strings"
	"testing"
)

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
	report, err := validateKnowledgeExtractReport(validKnowledgeArgs())
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
	if _, err := validateKnowledgeExtractReport(args); err == nil {
		t.Fatal("expected error for empty markdown with status ok")
	}
}

func TestValidateKnowledgeExtractReportRejectsEmptyWithoutReason(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "empty"
	args["markdown"] = ""
	// không có reason → phải từ chối, agent phải giải thích
	if _, err := validateKnowledgeExtractReport(args); err == nil {
		t.Fatal("expected error for status empty without reason")
	}
}

func TestValidateKnowledgeExtractReportAcceptsEmptyWithReason(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "empty"
	args["markdown"] = ""
	args["reason"] = "Trang PDF trắng, không có nội dung nào để trích."
	if _, err := validateKnowledgeExtractReport(args); err != nil {
		t.Fatalf("expected empty+reason to be accepted, got: %v", err)
	}
}

func TestValidateKnowledgeExtractReportRejectsBadStatus(t *testing.T) {
	args := validKnowledgeArgs()
	args["status"] = "done"
	if _, err := validateKnowledgeExtractReport(args); err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestValidateKnowledgeExtractReportRejectsMissingToolHealth(t *testing.T) {
	args := validKnowledgeArgs()
	delete(args, "tool_health")
	if _, err := validateKnowledgeExtractReport(args); err == nil {
		t.Fatal("expected error for missing tool_health")
	}
}

func TestKnowledgeExtractPromptMentionsFileURL(t *testing.T) {
	prompt := buildKnowledgeExtractPrompt(map[string]any{
		"file_url": "https://tekshot.localhost/sites/default/files/studio/knowledge/12/bang-gia.pdf",
		"mime":     "application/pdf",
		"filename": "bang-gia.pdf",
	})
	if !strings.Contains(prompt, "markitdown") || !strings.Contains(prompt, knowledgeFinalToolName) {
		t.Fatal("prompt must instruct markitdown strategy and name the collector tool")
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
