package tekshot

import (
	"strings"
	"testing"
)

func localProfileRequest() map[string]any {
	return map[string]any{
		"store_name": "Xây dựng",
		"business_profile": map[string]any{
			"name":        "Pizza Hip'S Thanh Trì",
			"goal":        "sell",
			"kind":        "physical",
			"description": "Quán pizza phục vụ khách gia đình quanh Thanh Trì",
			"offerings":   []any{"Pizza", "Mỳ Ý"},
			"channels":    []any{"dine_in", "takeaway"},
			"price_band":  map[string]any{"min": float64(80000), "max": float64(300000)},
			"geography": map[string]any{
				"mode":      "local",
				"lat":       20.9435,
				"lng":       105.8412,
				"radius_km": float64(3),
			},
			"notes": "Đối thủ hay bị nhầm với quán cùng tên ở quận khác",
		},
	}
}

func TestReadBusinessProfileAbsent(t *testing.T) {
	profile := readBusinessProfile(map[string]any{"store_name": "Xây dựng"})
	if profile.present {
		t.Fatal("a request without business_profile must not report one")
	}

	var sb strings.Builder
	if profile.writeProfile(&sb) {
		t.Fatal("writeProfile must report false so callers keep the fallback wording")
	}
	if sb.String() != "" {
		t.Fatalf("nothing should have been written, got %q", sb.String())
	}
}

func TestMarketResearchPromptFallsBackWithoutProfile(t *testing.T) {
	prompt := buildMarketResearchPrompt(map[string]any{"store_name": "Xây dựng", "locality": "Hà Nội"})
	if !strings.Contains(prompt, "derive only from the supplied store context") {
		t.Fatal("an undeclared subject must keep the legacy infer-it-yourself wording")
	}
}

func TestMarketResearchPromptUsesDeclaredSubject(t *testing.T) {
	prompt := buildMarketResearchPrompt(localProfileRequest())

	// The declared brand replaces the store name, which here is an internal
	// category ("Xây dựng") rather than a business.
	if !strings.Contains(prompt, "Subject: Pizza Hip'S Thanh Trì") {
		t.Fatalf("declared name missing:\n%s", prompt)
	}
	if strings.Contains(prompt, "derive only from the supplied store context") {
		t.Fatal("the fallback wording must disappear once a profile is declared")
	}
	for _, want := range []string{
		"Quán pizza phục vụ khách gia đình",
		"Main products/services: Pizza; Mỳ Ý",
		"Typical order value: 80000–300000 VND",
		"within about 3.0 km",
		"Team notes:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestDiscoveryPromptDropsFoodHardcodeAndFollowsGoal(t *testing.T) {
	cases := []struct {
		goal    string
		want    string
		unwant  string
		geoMode string
	}{
		{goal: "sell", want: "compete for the SAME customers", unwant: "food/service", geoMode: "local"},
		{goal: "recruit", want: "competing for the SAME candidates", unwant: "SAME customers", geoMode: "area"},
		{goal: "leads", want: "competing for the SAME sign-ups", unwant: "SAME customers", geoMode: "area"},
		{goal: "community", want: "SAME audience attention", unwant: "SAME customers", geoMode: "nationwide"},
	}

	for _, tc := range cases {
		request := localProfileRequest()
		profile := request["business_profile"].(map[string]any)
		profile["goal"] = tc.goal
		profile["geography"] = map[string]any{"mode": tc.geoMode, "area": "Hà Nội và lân cận"}

		prompt := buildCompetitorDiscoveryPrompt(request)
		if !strings.Contains(prompt, tc.want) {
			t.Fatalf("goal %s: missing %q:\n%s", tc.goal, tc.want, prompt)
		}
		if strings.Contains(prompt, tc.unwant) {
			t.Fatalf("goal %s: must not contain %q", tc.goal, tc.unwant)
		}
	}
}

func TestDiscoveryReachFollowsGeographyMode(t *testing.T) {
	cases := map[string]string{
		"nationwide": "NO physical catchment",
		"area":       "Search across Hà Nội và lân cận",
		"local":      "Stay within roughly",
	}

	for mode, want := range cases {
		request := localProfileRequest()
		profile := request["business_profile"].(map[string]any)
		profile["geography"] = map[string]any{
			"mode":      mode,
			"area":      "Hà Nội và lân cận",
			"lat":       20.9435,
			"lng":       105.8412,
			"radius_km": float64(5),
		}

		prompt := buildCompetitorDiscoveryPrompt(request)
		if !strings.Contains(prompt, want) {
			t.Fatalf("mode %s: missing %q:\n%s", mode, want, prompt)
		}
	}
}

func TestPosSnapshotRendersOnlyWhenSent(t *testing.T) {
	request := localProfileRequest()
	prompt := buildMarketResearchPrompt(request)
	if strings.Contains(prompt, "Actual sales") {
		t.Fatal("no pos_snapshot was sent, so no sales line may appear")
	}

	profile := request["business_profile"].(map[string]any)
	profile["pos_snapshot"] = map[string]any{
		"window_days":  float64(30),
		"orders":       float64(738),
		"aov":          float64(123988),
		"peak_hours":   []any{float64(19), float64(18)},
		"receive_mix":  map[string]any{"Tại chỗ": float64(42), "Mang đi": float64(58)},
		"top_products": []any{map[string]any{"title": "Combo 2 Người"}},
	}

	prompt = buildMarketResearchPrompt(request)
	for _, want := range []string{
		"738 orders in the last 30 days",
		"average order 123988 VND",
		"busiest hours 19h, 18h",
		// Sorted, so the line is identical on every run.
		"Order mix: Mang đi 58%, Tại chỗ 42%",
		"Best sellers right now: Combo 2 Người",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q:\n%s", want, prompt)
		}
	}
}

func TestRecruitProfileRelabelsPriceAndOfferings(t *testing.T) {
	request := localProfileRequest()
	profile := request["business_profile"].(map[string]any)
	profile["goal"] = "recruit"
	profile["offerings"] = []any{"Nhân viên bán hàng", "Bếp trưởng"}

	prompt := buildMarketResearchPrompt(request)
	if !strings.Contains(prompt, "Roles being hired: Nhân viên bán hàng; Bếp trưởng") {
		t.Fatalf("offerings must be relabelled for a recruiting page:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Salary range offered") {
		t.Fatalf("the money band must read as salary for a recruiting page:\n%s", prompt)
	}
	if !strings.Contains(prompt, "labour market") {
		t.Fatalf("trends must point at the labour market:\n%s", prompt)
	}
}

func TestAdsPromptCarriesTheSubject(t *testing.T) {
	request := localProfileRequest()
	request["competitors"] = []any{"Pizza Hut Thanh Trì"}

	prompt := buildCompetitorAdsPrompt(request)
	if !strings.Contains(prompt, "Subject: Pizza Hip'S Thanh Trì") {
		t.Fatalf("ads prompt missing the declared subject:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Quán pizza phục vụ khách gia đình") {
		t.Fatalf("ads prompt missing the declared description:\n%s", prompt)
	}
}
