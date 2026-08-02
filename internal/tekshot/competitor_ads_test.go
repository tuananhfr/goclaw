package tekshot

import (
	"strings"
	"testing"
)

func validAdCard() map[string]any {
	return map[string]any{
		"library_id":           "2030055207623142",
		"status":               "active",
		"start_date":           "2026-06-06",
		"end_date":             "",
		"platforms":            []any{"facebook", "instagram"},
		"media_type":           "video",
		"body":                 "Mua 1 tặng 1 pizza size L, chỉ trong tuần này.",
		"cta_link":             "https://example.com/uu-dai",
		"creative_reuse_count": float64(2),
	}
}

func validAdsCompetitor() map[string]any {
	return map[string]any{
		"name":             "Amor Pizza & Trà Sữa",
		"status":           "has_ads",
		"ad_library_url":   "https://www.facebook.com/ads/library/?active_status=all&ad_type=all&country=VN&view_all_page_id=123456789",
		"page_id":          "123456789",
		"active_ads_count": float64(3),
		"ads":              []any{validAdCard()},
		"observations":     "Chạy đều khuyến mãi mua 1 tặng 1, tập trung video ngắn.",
	}
}

func validAdsReport() map[string]any {
	return map[string]any{
		"competitors": []any{validAdsCompetitor()},
		"summary":     "Đối thủ gần nhất đang đẩy khuyến mãi combo cuối tuần.",
		"tool_health": map[string]any{"browser": "ok", "web_search": "ok", "notes": ""},
	}
}

func TestValidateCompetitorAdsAcceptsValidReport(t *testing.T) {
	if _, err := validateCompetitorAds(validAdsReport()); err != nil {
		t.Fatalf("expected valid report to pass, got: %v", err)
	}
}

func TestValidateCompetitorAdsAcceptsNoAdsAsARealFinding(t *testing.T) {
	// "This rival is not spending" is exactly the answer a marketer wants;
	// it must pass as long as the agent proves it opened the library.
	report := validAdsReport()
	competitor := validAdsCompetitor()
	competitor["status"] = "no_ads"
	competitor["ads"] = []any{}
	competitor["active_ads_count"] = float64(0)
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err != nil {
		t.Fatalf("expected 'no_ads' to pass, got: %v", err)
	}
}

func TestValidateCompetitorAdsRequiresLibraryURLForLookedAtStatuses(t *testing.T) {
	// 'has_ads' and 'no_ads' both claim "I opened the page". The URL is the
	// only evidence of that claim.
	for _, status := range []string{"has_ads", "no_ads"} {
		report := validAdsReport()
		competitor := validAdsCompetitor()
		competitor["status"] = status
		competitor["ad_library_url"] = ""
		if status == "no_ads" {
			competitor["ads"] = []any{}
		}
		report["competitors"] = []any{competitor}
		if _, err := validateCompetitorAds(report); err == nil {
			t.Fatalf("expected missing ad_library_url to be rejected for status %q", status)
		}
	}
}

func TestValidateCompetitorAdsRejectsNonLibraryURL(t *testing.T) {
	report := validAdsReport()
	competitor := validAdsCompetitor()
	competitor["ad_library_url"] = "https://example.com/blog/competitor-ads"
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected a non-Ad-Library URL to be rejected")
	}
}

func TestValidateCompetitorAdsKeepsStatusAndPayloadConsistent(t *testing.T) {
	// has_ads with nothing attached, or a non-has_ads status carrying ads,
	// means the label was guessed rather than observed.
	report := validAdsReport()
	competitor := validAdsCompetitor()
	competitor["ads"] = []any{}
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected 'has_ads' with no ads to be rejected")
	}

	report = validAdsReport()
	competitor = validAdsCompetitor()
	competitor["status"] = "not_found"
	competitor["ad_library_url"] = ""
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected a non-'has_ads' status carrying ads to be rejected")
	}
}

func TestValidateCompetitorAdsRejectsAllBlockedWhileBrowserOk(t *testing.T) {
	// The module-wide invariant, one layer down: empty-because-blocked and
	// empty-because-no-ads must never look the same.
	report := validAdsReport()
	competitor := validAdsCompetitor()
	competitor["status"] = "blocked"
	competitor["ad_library_url"] = ""
	competitor["ads"] = []any{}
	report["competitors"] = []any{competitor}
	_, err := validateCompetitorAds(report)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected all-blocked with browser 'ok' to be rejected, got: %v", err)
	}

	// Same report is valid once the agent admits the browser struggled.
	report["tool_health"] = map[string]any{"browser": "degraded", "web_search": "ok", "notes": "Ad Library đòi đăng nhập."}
	if _, err := validateCompetitorAds(report); err != nil {
		t.Fatalf("expected all-blocked with browser 'degraded' to pass, got: %v", err)
	}
}

func TestValidateCompetitorAdsEnforcesLibraryIDShape(t *testing.T) {
	for _, bad := range []string{"", "abc", "ID thư viện 123", "12345"} {
		report := validAdsReport()
		competitor := validAdsCompetitor()
		ad := validAdCard()
		ad["library_id"] = bad
		competitor["ads"] = []any{ad}
		report["competitors"] = []any{competitor}
		if _, err := validateCompetitorAds(report); err == nil {
			t.Fatalf("expected library_id %q to be rejected", bad)
		}
	}
}

func TestValidateCompetitorAdsRequiresISODates(t *testing.T) {
	// The card shows "6 Tháng 6, 2026"; anything the agent failed to convert
	// must not reach Drupal as a date.
	report := validAdsReport()
	competitor := validAdsCompetitor()
	ad := validAdCard()
	ad["start_date"] = "6 Tháng 6, 2026"
	competitor["ads"] = []any{ad}
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected a localised date to be rejected")
	}
}

func TestValidateCompetitorAdsAllowsEmptyPlatforms(t *testing.T) {
	// Platform icons are frequently unreadable in an accessibility snapshot.
	// An empty array is honest; failing the whole run over it is not.
	report := validAdsReport()
	competitor := validAdsCompetitor()
	ad := validAdCard()
	ad["platforms"] = []any{}
	competitor["ads"] = []any{ad}
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err != nil {
		t.Fatalf("expected empty platforms to pass, got: %v", err)
	}
}

func TestValidateCompetitorAdsEnforcesVocabularies(t *testing.T) {
	report := validAdsReport()
	competitor := validAdsCompetitor()
	competitor["status"] = "maybe"
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected an unknown competitor status to be rejected")
	}

	report = validAdsReport()
	competitor = validAdsCompetitor()
	ad := validAdCard()
	ad["media_type"] = "gif"
	competitor["ads"] = []any{ad}
	report["competitors"] = []any{competitor}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected an unknown media_type to be rejected")
	}

	report = validAdsReport()
	report["tool_health"] = map[string]any{"browser": "sometimes", "web_search": "ok", "notes": ""}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected an unknown browser health to be rejected")
	}
}

func TestValidateCompetitorAdsRejectsDroppedCompetitors(t *testing.T) {
	report := validAdsReport()
	report["competitors"] = []any{}
	if _, err := validateCompetitorAds(report); err == nil {
		t.Fatal("expected an empty competitors array to be rejected")
	}
}

func TestCompetitorAdsToolAllowIncludesBrowser(t *testing.T) {
	// web_fetch alone cannot read the Ad Library — it is a JS app behind a
	// cookie dialog. Losing 'browser' here would turn every run into an
	// empty report that still looks successful.
	allowed := tekshotCompetitorAdsToolAllow()
	for _, name := range []string{"web_search", "web_fetch", "browser", "datetime"} {
		if !containsString(allowed, name) {
			t.Fatalf("expected %q in the competitor ads tool allowlist", name)
		}
	}
	for _, forbidden := range []string{"exec", "write_file", "facebook_post_with_comments"} {
		if containsString(allowed, forbidden) {
			t.Fatalf("competitor ads must not be allowed to call %q", forbidden)
		}
	}
}

func TestBuildCompetitorAdsPromptCarriesTheURLRecipe(t *testing.T) {
	// The agent must not have to discover the Ad Library URL shape by trial
	// and error — browser steps are the scarce resource in a 12' run.
	prompt := buildCompetitorAdsPrompt(map[string]any{
		"store_name": "Pizza Hip'S Đại Áng",
		"industry":   "Pizza",
		"locality":   "Đại Áng, Thanh Trì",
		"competitors": []any{
			map[string]any{"name": "Amor Pizza & Trà Sữa", "facebook_url": "https://www.facebook.com/amorpizzavn/", "label": "direct"},
		},
	})
	for _, needle := range []string{
		"facebook.com/ads/library",
		"view_all_page_id",
		"country=VN",
		"Amor Pizza & Trà Sữa",
		"https://www.facebook.com/amorpizzavn/",
		competitorAdsFinalToolName,
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to contain %q", needle)
		}
	}
}

func TestBuildCompetitorAdsPromptHonoursCountryOverride(t *testing.T) {
	prompt := buildCompetitorAdsPrompt(map[string]any{
		"industry":     "Coffee",
		"country_code": "SG",
		"competitors":  []any{map[string]any{"name": "Some Cafe"}},
	})
	if !strings.Contains(prompt, "country=SG") {
		t.Fatal("expected the country override to reach the URL recipe")
	}
}
