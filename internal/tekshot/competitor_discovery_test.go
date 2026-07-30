package tekshot

import (
	"strings"
	"testing"
)

func validCompetitorItem() map[string]any {
	return map[string]any{
		"name":             "Pizza Hut Thanh Trì",
		"why_relevant":     "Cùng bán pizza, cách cửa hàng khoảng 1km, phục vụ cùng nhóm khách gia đình.",
		"label_suggestion": "direct",
		"website":          "https://pizzahut.vn",
		"facebook_url":     "",
		"locality":         "Thanh Trì, Hà Nội",
		"sources":          []any{"https://example.com/pizza-hut-thanh-tri"},
	}
}

func validCompetitorReport() map[string]any {
	return map[string]any{
		"competitors": []any{validCompetitorItem()},
		"reason":      "",
		"tool_health": map[string]any{"web_search": "ok", "notes": ""},
	}
}

func TestValidateCompetitorDiscoveryAcceptsValidReport(t *testing.T) {
	if _, err := validateCompetitorDiscovery(validCompetitorReport()); err != nil {
		t.Fatalf("expected valid report to pass, got: %v", err)
	}
}

func TestValidateCompetitorDiscoveryAllowsEmptyListWithReason(t *testing.T) {
	// A small locality genuinely having no comparable business is a real
	// answer, unlike market research where all-empty means a lazy run.
	report := validCompetitorReport()
	report["competitors"] = []any{}
	report["reason"] = "Khu vực chỉ có quán ăn gia đình, không có cửa hàng pizza nào trong bán kính 3km."
	if _, err := validateCompetitorDiscovery(report); err != nil {
		t.Fatalf("expected empty list with a reason to pass, got: %v", err)
	}
}

func TestValidateCompetitorDiscoveryRejectsSilentEmptyList(t *testing.T) {
	report := validCompetitorReport()
	report["competitors"] = []any{}
	report["reason"] = "   "
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected an empty list without a reason to be rejected")
	}
}

func TestValidateCompetitorDiscoveryRequiresNameAndReason(t *testing.T) {
	for _, field := range []string{"name", "why_relevant"} {
		report := validCompetitorReport()
		item := validCompetitorItem()
		item[field] = " "
		report["competitors"] = []any{item}
		_, err := validateCompetitorDiscovery(report)
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("expected blank %s to be rejected, got: %v", field, err)
		}
	}
}

func TestValidateCompetitorDiscoveryEnforcesLabelVocabulary(t *testing.T) {
	report := validCompetitorReport()
	item := validCompetitorItem()
	item["label_suggestion"] = "maybe"
	report["competitors"] = []any{item}
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected an unknown label to be rejected")
	}

	for _, label := range competitorLabels {
		report := validCompetitorReport()
		item := validCompetitorItem()
		item["label_suggestion"] = label
		report["competitors"] = []any{item}
		if _, err := validateCompetitorDiscovery(report); err != nil {
			t.Fatalf("expected label %q to pass, got: %v", label, err)
		}
	}
}

func TestValidateCompetitorDiscoveryRequiresRealSources(t *testing.T) {
	report := validCompetitorReport()
	item := validCompetitorItem()
	item["sources"] = []any{}
	report["competitors"] = []any{item}
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected a competitor without sources to be rejected")
	}

	report = validCompetitorReport()
	item = validCompetitorItem()
	item["sources"] = []any{"not-a-url"}
	report["competitors"] = []any{item}
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected a non-http source to be rejected")
	}
}

func TestValidateCompetitorDiscoveryLinksMayBeEmptyButNotGarbage(t *testing.T) {
	report := validCompetitorReport()
	item := validCompetitorItem()
	item["website"] = ""
	item["facebook_url"] = ""
	report["competitors"] = []any{item}
	if _, err := validateCompetitorDiscovery(report); err != nil {
		t.Fatalf("expected empty optional links to pass, got: %v", err)
	}

	report = validCompetitorReport()
	item = validCompetitorItem()
	item["facebook_url"] = "facebook.com/guessed-page"
	report["competitors"] = []any{item}
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected a non-http facebook_url to be rejected")
	}
}

func TestValidateCompetitorDiscoveryRejectsTooMany(t *testing.T) {
	report := validCompetitorReport()
	items := make([]any, 0, competitorMaxItems+1)
	for i := 0; i <= competitorMaxItems; i++ {
		items = append(items, validCompetitorItem())
	}
	report["competitors"] = items
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected more than the cap to be rejected")
	}
}

func TestValidateCompetitorDiscoveryChecksToolHealth(t *testing.T) {
	report := validCompetitorReport()
	report["tool_health"] = map[string]any{"web_search": "broken", "notes": ""}
	if _, err := validateCompetitorDiscovery(report); err == nil {
		t.Fatal("expected an unknown tool_health value to be rejected")
	}
}

func TestMarketResearchPromptCarriesApprovedCompetitors(t *testing.T) {
	prompt := buildMarketResearchPrompt(map[string]any{
		"industry": "Pizza / đồ ăn nhanh",
		"locality": "Thanh Trì, Hà Nội",
		"competitors": []any{
			map[string]any{"name": "Pizza Hut Thanh Trì", "label": "direct", "website": "https://pizzahut.vn"},
			map[string]any{"name": "Liên Sỹ Food", "label": "reference"},
		},
	})
	if !strings.Contains(prompt, "Competitors the store's team has confirmed") {
		t.Fatal("expected the approved competitor block")
	}
	if !strings.Contains(prompt, "Pizza Hut Thanh Trì (direct competitor)") {
		t.Fatal("expected direct competitors to be labelled as such")
	}
	if !strings.Contains(prompt, "Liên Sỹ Food (watch for content ideas only)") {
		t.Fatal("expected reference competitors to be labelled as such")
	}
	if !strings.Contains(prompt, "local_signals MUST check what those specific businesses") {
		t.Fatal("expected the local_signals instruction when a list is present")
	}

	// Without a list the prompt must stay exactly as it was before.
	bare := buildMarketResearchPrompt(map[string]any{"industry": "Pizza"})
	if strings.Contains(bare, "Competitors the store's team has confirmed") ||
		strings.Contains(bare, "local_signals MUST check") {
		t.Fatal("expected no competitor block when the store has none")
	}
}

func TestBuildCompetitorDiscoveryPromptExcludesOwnBusiness(t *testing.T) {
	// Regression: on 2026-07-30 the agent proposed the store itself after
	// finding its own Facebook page.
	prompt := buildCompetitorDiscoveryPrompt(map[string]any{
		"industry": "Pizza / đồ ăn nhanh",
		"locality": "Chư Sê, Gia Lai",
		"own_identity": []any{
			"Pizza Hip's Chư Sê",
			"Pizza Hip'S Đại Áng",
		},
	})
	if !strings.Contains(prompt, "This IS the business") {
		t.Fatal("expected the own-identity block")
	}
	if !strings.Contains(prompt, "Pizza Hip's Chư Sê") || !strings.Contains(prompt, "Pizza Hip'S Đại Áng") {
		t.Fatal("expected the store and its sibling branches to be listed")
	}
	if !strings.Contains(prompt, "NEVER propose the store itself") {
		t.Fatal("expected the hard rule against self-proposals")
	}

	bare := buildCompetitorDiscoveryPrompt(map[string]any{"industry": "Pizza"})
	if strings.Contains(bare, "This IS the business") {
		t.Fatal("expected no own-identity block when none was supplied")
	}
}

func TestBuildCompetitorDiscoveryPromptListsKnownNames(t *testing.T) {
	prompt := buildCompetitorDiscoveryPrompt(map[string]any{
		"industry":             "Pizza / đồ ăn nhanh",
		"store_name":           "Pizza Hip'S Đại Áng",
		"locality":             "Thanh Trì, Hà Nội",
		"existing_competitors": []any{"Pizza Hut Thanh Trì", "Quán Ăn Ngon"},
	})
	if !strings.Contains(prompt, "do NOT propose these again") {
		t.Fatal("expected the prompt to carry the already-known list")
	}
	if !strings.Contains(prompt, "Pizza Hut Thanh Trì") || !strings.Contains(prompt, "Quán Ăn Ngon") {
		t.Fatal("expected each known competitor to be listed")
	}

	bare := buildCompetitorDiscoveryPrompt(map[string]any{"industry": "Pizza"})
	if strings.Contains(bare, "do NOT propose these again") {
		t.Fatal("expected no exclusion block when the store has no competitors yet")
	}
}
