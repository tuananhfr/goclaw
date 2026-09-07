package tekshot

import (
	"strings"
	"testing"
)

func TestNormalizeBlogAuditFailsClosedOnUnknownValues(t *testing.T) {
	raw := map[string]any{
		"seo_score":            float64(140),
		"ai_readability_score": float64(-3),
		"issues": []any{
			map[string]any{"code": "x", "severity": "meh", "message": "Thiếu câu trả lời"},
			map[string]any{"code": "y", "severity": "info", "message": ""},
		},
		"suggestions": []any{
			map[string]any{"title": "", "instruction": "Rút ngắn phần 2", "scope": "section:s9"},
			map[string]any{"instruction": "Đổi layout", "scope": "presentation"},
			map[string]any{"instruction": "", "scope": "all"},
		},
	}
	out := normalizeBlogAudit(raw, map[string]bool{"s1": true, "s2": true})
	if out["seo_score"] != 100 || out["ai_readability_score"] != 0 {
		t.Fatalf("scores not clamped: %v %v", out["seo_score"], out["ai_readability_score"])
	}
	issues := out["issues"].([]any)
	if len(issues) != 1 || issues[0].(map[string]any)["severity"] != "error" {
		t.Fatalf("unknown severity must become error, empty message dropped: %v", issues)
	}
	suggestions := out["suggestions"].([]any)
	if len(suggestions) != 2 {
		t.Fatalf("empty instruction must be dropped: %v", suggestions)
	}
	first := suggestions[0].(map[string]any)
	if first["scope"] != "all" || first["title"] != "Rút ngắn phần 2" {
		t.Fatalf("unknown section must fall back to all and title default to the instruction: %v", first)
	}
	if suggestions[1].(map[string]any)["scope"] != "presentation" {
		t.Fatal("presentation scope must be kept")
	}
}

func TestParseBlogAuditReplyAcceptsFencedJSON(t *testing.T) {
	parsed, err := parseBlogAuditReply("Sure:\n```json\n{\"seo_score\": 70, \"issues\": []}\n```")
	if err != nil || parsed["seo_score"] != float64(70) {
		t.Fatalf("unexpected: %v %v", parsed, err)
	}
	if _, err := parseBlogAuditReply("no json here"); err == nil {
		t.Fatal("prose must not parse")
	}
}

func TestAuditUnreadableResultBlocksPublish(t *testing.T) {
	out := auditUnreadableResult("boom")
	issues := out["issues"].([]any)
	if len(issues) != 1 || issues[0].(map[string]any)["code"] != blogAuditUnread || issues[0].(map[string]any)["severity"] != "error" {
		t.Fatalf("expected one blocking audit_unreadable issue: %v", issues)
	}
	if out["seo_score"] != 0 {
		t.Fatal("score must be zero")
	}
}

func TestBuildBlogAuditPromptIsReadOnlyAndNamesTheRules(t *testing.T) {
	prompt := buildBlogAuditPrompt(map[string]any{
		"document":  rewriteBaseDocument(),
		"seo_rules": []any{map[string]any{"code": "no_faq"}},
		"snapshot":  map[string]any{"website": map[string]any{"language": "vi"}},
	})
	for _, want := range []string{"SEO RULES", "no_faq", "## DOCUMENT", "\"id\":\"s1\"", "language \"vi\""} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(prompt, "submit_blog") {
		t.Fatal("audit must not ask for a document tool")
	}
}
