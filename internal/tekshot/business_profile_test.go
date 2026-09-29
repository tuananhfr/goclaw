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

func fullProfileRequest() map[string]any {
	request := localProfileRequest()
	profile := request["business_profile"].(map[string]any)
	profile["positioning"] = "Pizza nướng lửa giá gia đình"
	profile["page_role"] = "Page của chính quán, nhận đặt bàn"
	profile["audiences"] = []any{
		map[string]any{"name": "Gia đình trẻ", "needs": "bữa tối nhanh", "hesitations": "giá"},
		map[string]any{"name": "", "needs": "dropped without a name"},
	}
	profile["proof"] = []any{"Ảnh bếp thật"}
	profile["redirects"] = []any{"Trang nhượng quyền — khi khách hỏi mở quán"}
	profile["contact"] = map[string]any{"website": "vidu.example.com", "hotline": "0900000000"}
	profile["store"] = map[string]any{"name": "Quán Mẫu Chi nhánh 1", "address": "1 Phố Mẫu", "hours": "Thứ 2–Chủ nhật 08:00–23:00"}
	return request
}

func TestWriteProfileRendersDeclaredIdentityFields(t *testing.T) {
	var sb strings.Builder
	readBusinessProfile(fullProfileRequest()).writeProfile(&sb)
	text := sb.String()

	for _, want := range []string{
		"- Positioning: Pizza nướng lửa giá gia đình",
		"- Role of this page: Page của chính quán, nhận đặt bàn",
		"- Main customers:\n  - Gia đình trẻ — needs/worries: bữa tối nhanh — still hesitant about: giá\n",
		"- Proof available: Ảnh bếp thật",
		"- May send people on to: Trang nhượng quyền — khi khách hỏi mở quán",
		// Fixed order, whatever order the map arrived in.
		"- Contact: Hotline 0900000000, Website vidu.example.com",
		"- The shop this page speaks for: Quán Mẫu Chi nhánh 1 — 1 Phố Mẫu — open Thứ 2–Chủ nhật 08:00–23:00",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "dropped without a name") {
		t.Fatal("an audience without a name must be skipped")
	}
}

func TestUnlinkedProfileMentionsNoShop(t *testing.T) {
	var sb strings.Builder
	readBusinessProfile(localProfileRequest()).writeProfile(&sb)
	if strings.Contains(sb.String(), "The shop this page speaks for") {
		t.Fatal("no store block was sent, so no shop line may appear")
	}
}

func TestNewGoalsHaveTheirOwnWording(t *testing.T) {
	cases := map[string][]string{
		goalDealer:    {"BUSINESS PARTNERS", "Products offered to dealers"},
		goalFranchise: {"INVESTORS", "Franchise packages offered", "Investment range"},
		goalTraffic:   {"send readers to its website", "Content topics"},
	}
	for goal, wants := range cases {
		request := localProfileRequest()
		request["business_profile"].(map[string]any)["goal"] = goal
		var sb strings.Builder
		readBusinessProfile(request).writeProfile(&sb)
		for _, want := range wants {
			if !strings.Contains(sb.String(), want) {
				t.Fatalf("goal %s: missing %q:\n%s", goal, want, sb.String())
			}
		}
	}

	dealer := localProfileRequest()
	dealer["business_profile"].(map[string]any)["goal"] = goalDealer
	if !strings.Contains(buildCompetitorDiscoveryPrompt(dealer), "SAME dealers and distributors") {
		t.Fatal("dealer discovery must look for brands chasing the same dealers")
	}
}

func TestLocalGeographyWithoutCoordinatesUsesTheAddress(t *testing.T) {
	request := localProfileRequest()
	request["business_profile"].(map[string]any)["geography"] = map[string]any{"mode": "local", "radius_km": float64(3), "area": "1 Phố Mẫu, Hà Nội"}
	if got := readBusinessProfile(request).geoSentence(); got != "serves customers within about 3.0 km of 1 Phố Mẫu, Hà Nội" {
		t.Fatalf("unexpected geography: %q", got)
	}
}
