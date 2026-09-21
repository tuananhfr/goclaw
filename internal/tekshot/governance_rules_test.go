package tekshot

import (
	"reflect"
	"strings"
	"testing"
)

func requestWithRules(rules map[string]any) map[string]any {
	return map[string]any{"page_profile": map[string]any{
		"profile":      []any{"P1"},
		"system_rules": rules,
	}}
}

func TestResolveGovernanceRulesWithoutOverridesIsTheDefault(t *testing.T) {
	if !reflect.DeepEqual(resolveGovernanceRules(nil), defaultGovernanceRules()) {
		t.Fatal("no overrides must resolve to the defaults")
	}
}

func TestOverriddenRulesReachEveryPrompt(t *testing.T) {
	request := requestWithRules(map[string]any{
		"absolute_rules":      []any{map[string]any{"code": "1", "text": "LUẬT SỬA MỘT"}},
		"compliance_checks":   []any{map[string]any{"code": "C90", "profiles": []any{"P1"}, "rule": "MỤC C SỬA"}},
		"compliance_warnings": []any{map[string]any{"code": "W9", "text": "CẢNH BÁO SỬA"}},
		"image_rules":         "LUẬT ẢNH SỬA",
		"media_branch_rules":  map[string]any{"UPLOAD": "NHÁNH ẢNH THẬT SỬA"},
	})
	profile := pageProfileFromRequest(request)

	writing := buildGovernancePrompt(profile)
	if !strings.Contains(writing, "1. LUẬT SỬA MỘT") || strings.Contains(writing, "NGUỒN. Chỉ dùng") {
		t.Fatalf("writing prompt did not take the override:\n%s", writing)
	}

	compliance := buildCompliancePrompt(profile, map[string]any{})
	if !strings.Contains(compliance, "C90. MỤC C SỬA") || !strings.Contains(compliance, "W9. CẢNH BÁO SỬA") || strings.Contains(compliance, "C2. ") {
		t.Fatalf("compliance prompt did not take the override:\n%s", compliance)
	}

	request["loai_anh"] = "PRODUCT"
	image := imageGuidanceFor(request)
	if !strings.Contains(image, "LUẬT ẢNH SỬA") || !strings.Contains(image, "NHÁNH ẢNH THẬT SỬA") || strings.Contains(image, "TUYỆT ĐỐI KHÔNG sinh ảnh") {
		t.Fatalf("image guidance did not take the override:\n%s", image)
	}
}

func TestBrokenOverridesFallBackToTheDefaultBlock(t *testing.T) {
	defaults := defaultGovernanceRules()
	cases := map[string]any{
		"absolute_rules":    []any{},
		"compliance_checks": []any{map[string]any{"code": "C2", "profiles": []any{"P9"}, "rule": "x"}},
		"risk_levels":       []any{map[string]any{"code": "LOW", "text": "a"}, map[string]any{"code": "MEDIUM", "text": "b"}},
		"image_rules":       "   ",
		"preamble":          42,
	}
	rules := resolveGovernanceRules(cases)
	if !reflect.DeepEqual(rules, defaults) {
		t.Fatalf("every broken block must keep its default, got %+v", rules)
	}
}

func TestOneBadRowDropsTheWholeList(t *testing.T) {
	rules := resolveGovernanceRules(map[string]any{"absolute_rules": []any{
		map[string]any{"code": "1", "text": "ok"},
		map[string]any{"code": "1", "text": "trùng mã"},
	}})
	if !reflect.DeepEqual(rules.AbsoluteRules, absoluteRules) {
		t.Fatal("a duplicate code must reject the whole override, not half of it")
	}
}

func TestRiskLevelsAreReorderedToLowMediumHigh(t *testing.T) {
	rules := resolveGovernanceRules(map[string]any{"risk_levels": []any{
		map[string]any{"code": "HIGH", "text": "h"},
		map[string]any{"code": "low", "text": "l"},
		map[string]any{"code": "MEDIUM", "text": "m"},
	}})
	got := []string{rules.RiskLevels[0].Code, rules.RiskLevels[1].Code, rules.RiskLevels[2].Code}
	if !reflect.DeepEqual(got, []string{"LOW", "MEDIUM", "HIGH"}) || rules.RiskLevels[0].Text != "l" {
		t.Fatalf("risk levels = %+v", rules.RiskLevels)
	}
}

func TestMediaBranchOverrideKeepsUntouchedBranches(t *testing.T) {
	rules := resolveGovernanceRules(map[string]any{"media_branch_rules": map[string]any{"REF": "REF SỬA", "INFO": ""}})
	if rules.MediaBranchRules["REF"] != "REF SỬA" || rules.MediaBranchRules["INFO"] != defaultMediaBranchRules["INFO"] {
		t.Fatalf("media branches = %+v", rules.MediaBranchRules)
	}
}
