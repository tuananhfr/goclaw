package tekshot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildGovernanceCatalogCarriesEveryPromptBlock(t *testing.T) {
	catalog := BuildGovernanceCatalog()

	if catalog.Version != governanceVersion {
		t.Fatalf("version = %q", catalog.Version)
	}
	if len(catalog.AbsoluteRules) != 15 {
		t.Fatalf("absolute rules = %d, want 15", len(catalog.AbsoluteRules))
	}
	if len(catalog.RiskLevels) != 3 {
		t.Fatalf("risk levels = %d, want 3", len(catalog.RiskLevels))
	}
	if len(catalog.ComplianceChecks) != len(complianceChecks) {
		t.Fatalf("checks = %d, want %d", len(catalog.ComplianceChecks), len(complianceChecks))
	}
	if len(catalog.ComplianceWarnings) != len(complianceWarnings) {
		t.Fatalf("warnings = %d, want %d", len(catalog.ComplianceWarnings), len(complianceWarnings))
	}
	if !strings.HasPrefix(catalog.ImageRules, "=== LUẬT ẢNH BẮT BUỘC") {
		t.Fatalf("image rules not trimmed: %q", catalog.ImageRules[:40])
	}
	for _, branch := range []string{"UPLOAD", "REF", "INFO"} {
		if catalog.MediaBranchRules[branch] == "" {
			t.Fatalf("media branch %s empty", branch)
		}
	}

	// Prompt thật phải chứa từng luật đúng như catalog trả ra.
	prompt := buildGovernancePrompt(&pageProfile{Codes: []string{"P1"}})
	for _, rule := range catalog.AbsoluteRules {
		if !strings.Contains(prompt, rule.Code+". "+rule.Text) {
			t.Fatalf("prompt missing rule %s", rule.Code)
		}
	}
}

func TestBuildGovernanceCatalogEncodesEmptyProfilesAsArray(t *testing.T) {
	raw, err := json.Marshal(BuildGovernanceCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"profiles":null`) {
		t.Fatal("a check applying to every profile must encode profiles as [] not null")
	}
}
