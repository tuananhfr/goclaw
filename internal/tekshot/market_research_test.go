package tekshot

import (
	"strings"
	"testing"
)

func validResearchItem() map[string]any {
	return map[string]any{
		"title":            "Trà đậm vị đang lên",
		"summary":          "Xu hướng trà đậm vị tiếp tục tăng trong 2 tháng gần đây.",
		"why_it_matters":   "Store đồ uống có thể bắt trend với chi phí thấp.",
		"suggested_action": "Ra mắt 1 món trà đậm vị và đăng bài giới thiệu trong tuần này.",
		"sources":          []any{"https://example.com/tra-dam-vi"},
	}
}

func validResearchReport() map[string]any {
	section := func() map[string]any {
		return map[string]any{
			"status": "ok",
			"items":  []any{validResearchItem(), validResearchItem(), validResearchItem()},
		}
	}
	return map[string]any{
		"sections": map[string]any{
			"trends":         section(),
			"seasonal_hooks": section(),
			"local_signals":  section(),
		},
		"tool_health": map[string]any{
			"web_search": "ok",
			"notes":      "",
		},
	}
}

func TestValidateMarketResearchReportAcceptsValidReport(t *testing.T) {
	if _, err := validateMarketResearchReport(validResearchReport()); err != nil {
		t.Fatalf("expected valid report to pass, got: %v", err)
	}
}

func TestValidateMarketResearchReportRequiresAllSections(t *testing.T) {
	report := validResearchReport()
	delete(report["sections"].(map[string]any), "local_signals")
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected missing section to fail")
	}
}

func TestValidateMarketResearchReportRejectsUnknownStatus(t *testing.T) {
	report := validResearchReport()
	report["sections"].(map[string]any)["trends"].(map[string]any)["status"] = "partial"
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected unknown status to fail")
	}
}

func TestValidateMarketResearchReportOkSectionNeedsItems(t *testing.T) {
	report := validResearchReport()
	report["sections"].(map[string]any)["trends"].(map[string]any)["items"] = []any{}
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected ok section without items to fail")
	}
}

func TestValidateMarketResearchReportEmptySectionRules(t *testing.T) {
	report := validResearchReport()
	trends := report["sections"].(map[string]any)["trends"].(map[string]any)
	trends["status"] = "empty"
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected empty section with items to fail")
	}
	trends["items"] = []any{}
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected empty section without reason to fail")
	}
	trends["reason"] = "Không tìm thấy tin địa phương trong tuần."
	if _, err := validateMarketResearchReport(report); err != nil {
		t.Fatalf("expected honest empty section to pass, got: %v", err)
	}
}

func TestValidateMarketResearchReportRequiresItemFields(t *testing.T) {
	for _, field := range []string{"title", "summary", "why_it_matters", "suggested_action"} {
		report := validResearchReport()
		items := report["sections"].(map[string]any)["trends"].(map[string]any)["items"].([]any)
		items[0].(map[string]any)[field] = "  "
		if _, err := validateMarketResearchReport(report); err == nil {
			t.Fatalf("expected blank %s to fail", field)
		}
	}
}

func TestValidateMarketResearchReportRequiresRealSources(t *testing.T) {
	report := validResearchReport()
	item := report["sections"].(map[string]any)["trends"].(map[string]any)["items"].([]any)[0].(map[string]any)
	item["sources"] = []any{}
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected empty sources to fail")
	}
	item["sources"] = []any{"see google"}
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected non-URL source to fail")
	}
}

func TestValidateMarketResearchReportRejectsAllEmptyWithHealthyTools(t *testing.T) {
	report := validResearchReport()
	for _, key := range researchSectionKeys {
		section := report["sections"].(map[string]any)[key].(map[string]any)
		section["status"] = "empty"
		section["items"] = []any{}
		section["reason"] = "không có gì"
	}
	_, err := validateMarketResearchReport(report)
	if err == nil {
		t.Fatal("expected all-empty report with web_search ok to fail")
	}
	if !strings.Contains(err.Error(), "web tools failed") {
		t.Fatalf("expected the silent-empty guard message, got: %v", err)
	}

	// The same all-empty report IS valid when the agent admits tool failure —
	// the job-level dead-tool failure happens in runMarketResearch, not here.
	report["tool_health"].(map[string]any)["web_search"] = "dead"
	report["tool_health"].(map[string]any)["notes"] = "web_search lỗi liên tục"
	if _, err := validateMarketResearchReport(report); err != nil {
		t.Fatalf("expected all-empty report with dead tools to pass validation, got: %v", err)
	}
}

func TestValidateMarketResearchReportRequiresToolHealth(t *testing.T) {
	report := validResearchReport()
	delete(report, "tool_health")
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected missing tool_health to fail")
	}
	report["tool_health"] = map[string]any{"web_search": "sometimes", "notes": ""}
	if _, err := validateMarketResearchReport(report); err == nil {
		t.Fatal("expected unknown web_search health to fail")
	}
}

func TestTekshotResearchToolAllowIsOutwardOnly(t *testing.T) {
	allowed := tekshotResearchToolAllow()
	for _, name := range []string{"web_search", "web_fetch", "datetime"} {
		found := false
		for _, tool := range allowed {
			if tool == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %s in market research allow-list", name)
		}
	}
	for _, name := range []string{"create_image", "vault_search", "memory_search", "submit_draft_batch"} {
		for _, tool := range allowed {
			if tool == name {
				t.Fatalf("did not expect %s in market research allow-list", name)
			}
		}
	}
}
