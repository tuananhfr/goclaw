package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	competitorAdsFinalToolName = "submit_competitor_ads"
	// Browser work costs several turns per competitor (open, dismiss the
	// cookie dialog, snapshot, scroll, snapshot again) where the two
	// search-only jobs cost one or two. The 12' run timeout is the real
	// ceiling; this only stops a stuck loop from burning all of it on one
	// page.
	competitorAdsMaxIterations = 40
	// Budget, not ambition: 5 competitors x 15 ads is already at the edge of
	// what fits in 12 minutes of browser stepping.
	competitorAdsMaxItems   = 5
	competitorAdsMaxPerItem = 15
)

// competitorAdsStatuses is the per-competitor outcome vocabulary, and the whole
// point of the split: "this rival runs no ads" is a REAL finding (they are not
// spending) and must never be confused with "the Ad Library refused us".
var competitorAdsStatuses = []string{"has_ads", "no_ads", "not_found", "blocked"}

// competitorAdsMediaTypes keeps the creative shape in a fixed vocabulary so
// Drupal can group by it without normalising free text.
var competitorAdsMediaTypes = []string{"image", "video", "carousel", "text", "unknown"}

// adLibraryIDPattern — the "ID thư viện" printed on every Ad Library card is
// numeric. A model that invents an ad usually invents something that is not.
var adLibraryIDPattern = regexp.MustCompile(`^[0-9]{8,25}$`)

// adLibraryISODate — dates are shown localised ("6 Tháng 6, 2026"); the agent
// converts, and this rejects anything it failed to convert.
var adLibraryISODate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// tekshotCompetitorAdsToolAllow adds the browser to the outward-looking set.
// The Ad Library is a JavaScript app behind a cookie dialog, so web_fetch only
// ever gets an empty shell — GoClaw's own fetch tool says exactly that and
// points at browser automation. web_search stays, but only to resolve a
// competitor name to its page.
func tekshotCompetitorAdsToolAllow() []string {
	return []string{
		"web_search",
		"web_fetch",
		"browser",
		"datetime",
	}
}

func (s *JobService) runCompetitorAds(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	// Unlike discovery, this job has nothing to look up on its own: it reads
	// the APPROVED list. Drupal gates on this too, but a job that silently
	// researched "whoever the model thinks competes" would defeat the whole
	// human-approval gate.
	if !hasKnownCompetitors(request) {
		return nil, "", fmt.Errorf("competitor ads research requires at least one approved competitor")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewCompetitorAdsCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildCompetitorAdsPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotCompetitorAdsToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  competitorAdsMaxIterations,
		TraceName:      "tekshot competitor ads",
		TraceTags:      []string{"tekshot", "competitor_ads"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit what you gathered now by calling %s, even if some competitors could not be checked — mark those with the honest status. Do not answer with plain text.", competitorAdsFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: competitorAdsFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid competitor ads report")
	}

	// A dead browser must FAIL the job. Drupal turns a failed job into a
	// visible alert; a completed job holding an empty report would instead
	// read as "no competitor is advertising" — the opposite claim, and one a
	// marketer would act on.
	if health, ok := report["tool_health"].(map[string]any); ok {
		if stringFromMap(health, "browser") == "dead" {
			notes := strings.TrimSpace(stringFromMap(health, "notes"))
			if notes == "" {
				notes = "the browser tool was unavailable"
			}
			return nil, "", fmt.Errorf("ad library browsing unavailable: %s", notes)
		}
	}

	competitors, _ := report["competitors"].([]any)
	ads := 0
	for _, raw := range competitors {
		if item, ok := raw.(map[string]any); ok {
			if list, ok := item["ads"].([]any); ok {
				ads += len(list)
			}
		}
	}
	return report, fmt.Sprintf("Ad library scan covered %d competitor(s), %d ad(s)", len(competitors), ads), nil
}

// CompetitorAdsCollectorTool captures the one structured ads report.
type CompetitorAdsCollectorTool struct {
	report map[string]any
}

// NewCompetitorAdsCollectorTool builds the ephemeral collector.
func NewCompetitorAdsCollectorTool() *CompetitorAdsCollectorTool {
	return &CompetitorAdsCollectorTool{}
}

// Name implements tools.Tool.
func (t *CompetitorAdsCollectorTool) Name() string { return competitorAdsFinalToolName }

// Description implements tools.Tool.
func (t *CompetitorAdsCollectorTool) Description() string {
	return "Submit the competitor ad-library findings as validated structured JSON. Call this once when every competitor has been checked."
}

// Parameters implements tools.Tool.
func (t *CompetitorAdsCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"competitors": map[string]any{
				"type":        "array",
				"description": "One entry per competitor you were given, in the same order. Never drop a competitor: report the honest status instead.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"name": map[string]any{"type": "string", "description": "Competitor name exactly as it was given to you, so Drupal can match the row."},
						"status": map[string]any{
							"type":        "string",
							"description": "'has_ads' = the page is running or has run ads; 'no_ads' = the Ad Library page opened and is genuinely empty; 'not_found' = no Ad Library page could be resolved for this business; 'blocked' = the library refused, timed out, or demanded a login. NEVER use 'no_ads' for a page you could not open.",
						},
						"ad_library_url":   map[string]any{"type": "string", "description": "The Ad Library URL you ACTUALLY opened. Required for has_ads and no_ads — it is the proof you looked. Empty string otherwise."},
						"page_id":          map[string]any{"type": "string", "description": "Numeric Facebook page id when you resolved one, empty string otherwise. Never guess."},
						"active_ads_count": map[string]any{"type": "number", "description": "How many ads are currently 'Hoạt động' (active). 0 is a valid, meaningful answer."},
						"ads": map[string]any{
							"type":        "array",
							"description": "Newest first. Required and non-empty when status is 'has_ads'; MUST be empty for every other status.",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"library_id":           map[string]any{"type": "string", "description": "The numeric 'ID thư viện' / 'Library ID' printed on the card. Copy it exactly — it is the dedupe key across weekly runs. Never invent one."},
									"status":               map[string]any{"type": "string", "description": "'active' for Hoạt động, 'inactive' for Không hoạt động."},
									"start_date":           map[string]any{"type": "string", "description": "Start date converted to YYYY-MM-DD (the card shows e.g. '6 Tháng 6, 2026'). Empty string when the card does not show one."},
									"end_date":             map[string]any{"type": "string", "description": "End date as YYYY-MM-DD for finished ads; empty string while the ad is still running."},
									"platforms":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Platforms shown on the card (facebook, instagram, messenger, threads, audience_network). Empty array when the icons are not readable — do not guess."},
									"media_type":           map[string]any{"type": "string", "description": "image, video, carousel, text or unknown."},
									"body":                 map[string]any{"type": "string", "description": "The ad copy as written. Empty string for image-only creatives."},
									"cta_link":             map[string]any{"type": "string", "description": "Destination or CTA link shown on the card, empty string when absent."},
									"creative_reuse_count": map[string]any{"type": "number", "description": "From 'N quảng cáo sử dụng nội dung và văn bản này' — how many ads share this creative. 1 when the card says nothing."},
								},
								"required": []string{"library_id", "status", "start_date", "end_date", "platforms", "media_type", "body", "cta_link", "creative_reuse_count"},
							},
						},
						"observations": map[string]any{"type": "string", "description": "What a marketer should notice about this rival's advertising: angles, offers, cadence. Empty string when there is nothing to say."},
					},
					"required": []string{"name", "status", "ad_library_url", "page_id", "active_ads_count", "ads", "observations"},
				},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Short narrative comparing the rivals' advertising and what this store should do about it.",
			},
			"tool_health": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"browser": map[string]any{
						"type":        "string",
						"description": "'ok' when the Ad Library rendered; 'degraded' when some pages failed; 'dead' when browsing was impossible. Be honest — 'dead' is the CORRECT answer when the tool failed, and it fails the job on purpose.",
					},
					"web_search": map[string]any{
						"type":        "string",
						"description": "'ok', 'degraded' or 'dead' for the search tool used to resolve pages.",
					},
					"notes": map[string]any{"type": "string", "description": "Short note on tool problems, empty string when everything worked."},
				},
				"required": []string{"browser", "web_search", "notes"},
			},
		},
		"required": []string{"competitors", "summary", "tool_health"},
	}
}

// Execute implements tools.Tool.
func (t *CompetitorAdsCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateCompetitorAds(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured competitor ads report captured.")
}

// Report returns a deep copy of the captured report, nil when absent.
func (t *CompetitorAdsCollectorTool) Report() map[string]any {
	if t.report == nil {
		return nil
	}
	encoded, err := json.Marshal(t.report)
	if err != nil {
		return nil
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil
	}
	return clone
}

func validateCompetitorAds(args map[string]any) (map[string]any, error) {
	competitors, ok := args["competitors"].([]any)
	if !ok || len(competitors) == 0 {
		return nil, fmt.Errorf("competitors must be a non-empty array — report a status for every competitor instead of dropping it")
	}
	if len(competitors) > competitorAdsMaxItems {
		return nil, fmt.Errorf("at most %d competitors", competitorAdsMaxItems)
	}

	health, ok := args["tool_health"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("report must contain a tool_health object")
	}
	browserHealth := strings.TrimSpace(stringFromMap(health, "browser"))
	switch browserHealth {
	case "ok", "degraded", "dead":
	default:
		return nil, fmt.Errorf("tool_health.browser must be 'ok', 'degraded' or 'dead'")
	}
	switch strings.TrimSpace(stringFromMap(health, "web_search")) {
	case "ok", "degraded", "dead":
	default:
		return nil, fmt.Errorf("tool_health.web_search must be 'ok', 'degraded' or 'dead'")
	}

	blocked := 0
	for i, rawItem := range competitors {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("competitors[%d] must be an object", i)
		}
		if strings.TrimSpace(stringFromMap(item, "name")) == "" {
			return nil, fmt.Errorf("competitors[%d].name is required", i)
		}
		status := strings.TrimSpace(stringFromMap(item, "status"))
		if !containsString(competitorAdsStatuses, status) {
			return nil, fmt.Errorf("competitors[%d].status must be one of %s", i, strings.Join(competitorAdsStatuses, ", "))
		}
		if status == "blocked" {
			blocked++
		}

		libraryURL := strings.TrimSpace(stringFromMap(item, "ad_library_url"))
		if status == "has_ads" || status == "no_ads" {
			// Both claims assert "I opened the page and looked". The URL is
			// the only evidence of that, so it is mandatory for exactly these
			// two — and it must be an Ad Library URL, not some blog post.
			if libraryURL == "" {
				return nil, fmt.Errorf("competitors[%d].ad_library_url is required when status is '%s'", i, status)
			}
			if !strings.Contains(libraryURL, "facebook.com/ads/library") {
				return nil, fmt.Errorf("competitors[%d].ad_library_url must be a facebook.com/ads/library URL", i)
			}
		}
		if libraryURL != "" && !strings.HasPrefix(libraryURL, "http://") && !strings.HasPrefix(libraryURL, "https://") {
			return nil, fmt.Errorf("competitors[%d].ad_library_url must be an http(s) URL or an empty string", i)
		}

		ads, ok := item["ads"].([]any)
		if !ok {
			return nil, fmt.Errorf("competitors[%d].ads must be an array", i)
		}
		if len(ads) > competitorAdsMaxPerItem {
			return nil, fmt.Errorf("competitors[%d]: at most %d ads", i, competitorAdsMaxPerItem)
		}
		// The status and the payload must agree. "no_ads" with ads attached,
		// or "has_ads" with none, means the model is guessing at a label
		// rather than reporting what it saw.
		if status == "has_ads" && len(ads) == 0 {
			return nil, fmt.Errorf("competitors[%d]: status 'has_ads' requires at least one ad", i)
		}
		if status != "has_ads" && len(ads) > 0 {
			return nil, fmt.Errorf("competitors[%d]: only status 'has_ads' may carry ads", i)
		}
		for j, rawAd := range ads {
			if err := validateCompetitorAd(rawAd); err != nil {
				return nil, fmt.Errorf("competitors[%d].ads[%d]: %v", i, j, err)
			}
		}
	}

	// Same guard as market research, one layer down: every competitor coming
	// back "blocked" while the browser reports 'ok' is a contradiction. One of
	// the two is wrong, and silently storing it would look like a finished
	// report holding no findings.
	if blocked == len(competitors) && browserHealth == "ok" {
		return nil, fmt.Errorf("every competitor is 'blocked' while tool_health.browser is 'ok' — if the Ad Library refused you, browser must be 'degraded' or 'dead'")
	}

	return args, nil
}

func validateCompetitorAd(raw any) error {
	ad, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("must be an object")
	}
	libraryID := strings.TrimSpace(stringFromMap(ad, "library_id"))
	if !adLibraryIDPattern.MatchString(libraryID) {
		return fmt.Errorf("library_id must be the numeric Ad Library ID copied from the card")
	}
	switch strings.TrimSpace(stringFromMap(ad, "status")) {
	case "active", "inactive":
	default:
		return fmt.Errorf("status must be 'active' or 'inactive'")
	}
	for _, field := range []string{"start_date", "end_date"} {
		value := strings.TrimSpace(stringFromMap(ad, field))
		if value == "" {
			continue
		}
		if !adLibraryISODate.MatchString(value) {
			return fmt.Errorf("%s must be YYYY-MM-DD or an empty string", field)
		}
	}
	mediaType := strings.TrimSpace(stringFromMap(ad, "media_type"))
	if !containsString(competitorAdsMediaTypes, mediaType) {
		return fmt.Errorf("media_type must be one of %s", strings.Join(competitorAdsMediaTypes, ", "))
	}
	// Platforms are icon-only on the card. Unreadable icons are expected and
	// must not fail a run — an empty array is the honest answer, a guessed
	// list is not.
	if platforms, ok := ad["platforms"].([]any); ok {
		for _, rawPlatform := range platforms {
			if _, ok := rawPlatform.(string); !ok {
				return fmt.Errorf("platforms must be strings")
			}
		}
	} else if ad["platforms"] != nil {
		return fmt.Errorf("platforms must be an array")
	}
	return nil
}

func buildCompetitorAdsPrompt(request map[string]any) string {
	storeName := strings.TrimSpace(stringFromMap(request, "store_name"))
	industry := strings.TrimSpace(stringFromMap(request, "industry"))
	locality := strings.TrimSpace(stringFromMap(request, "locality"))
	language := strings.TrimSpace(stringFromMap(request, "language"))
	if language == "" {
		language = "vi"
	}
	country := strings.TrimSpace(stringFromMap(request, "country_code"))
	if country == "" {
		country = "VN"
	}
	today := strings.TrimSpace(stringFromMap(request, "today"))

	var sb strings.Builder
	sb.WriteString("You are an advertising analyst. Your job is to read the Meta Ad Library and report what the competitors of ONE store are advertising right now.\n")
	sb.WriteString("Use the browser tool, then submit ONE structured report by calling ")
	sb.WriteString(competitorAdsFinalToolName)
	sb.WriteString(" exactly once. Do not answer with plain text.\n\n")

	profile := readBusinessProfile(request)

	sb.WriteString("## Store context\n")
	if subject := profile.subjectName(storeName); subject != "" {
		sb.WriteString("- Subject: " + subject + "\n")
	}
	profile.writeProfile(&sb)
	if industry != "" {
		sb.WriteString("- Industry: " + industry + "\n")
	}
	if locality != "" {
		sb.WriteString("- Locality: " + locality + "\n")
	}
	if today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Competitors to check — these and ONLY these\n")
	if competitors, ok := request["competitors"].([]any); ok {
		for _, raw := range competitors {
			switch value := raw.(type) {
			case string:
				if name := strings.TrimSpace(value); name != "" {
					sb.WriteString("- " + name + "\n")
				}
			case map[string]any:
				name := strings.TrimSpace(stringFromMap(value, "name"))
				if name == "" {
					continue
				}
				line := "- " + name
				if fb := strings.TrimSpace(stringFromMap(value, "facebook_url")); fb != "" {
					line += " — Facebook: " + fb
				}
				if label := strings.TrimSpace(stringFromMap(value, "label")); label != "" {
					line += " (" + label + ")"
				}
				sb.WriteString(line + "\n")
			}
		}
	}
	sb.WriteString("A human approved this list. Do not add businesses to it and do not skip any: every name gets an entry in your report, even if the only honest entry is 'not_found'.\n\n")

	sb.WriteString("## How to read the Ad Library\n")
	sb.WriteString("Work one competitor at a time.\n")
	sb.WriteString("1. Resolve the business to its Facebook page. Use the Facebook URL above when it is given; otherwise use web_search. If you cannot resolve a page, the status is 'not_found' — move on.\n")
	sb.WriteString(fmt.Sprintf("2. Open the Ad Library for that page:\n   https://www.facebook.com/ads/library/?active_status=all&ad_type=all&country=%s&view_all_page_id=<PAGE_ID>\n", country))
	sb.WriteString(fmt.Sprintf("   When you have no page id, fall back to a name search:\n   https://www.facebook.com/ads/library/?active_status=all&ad_type=all&country=%s&q=<NAME>&search_type=keyword_unordered\n", country))
	sb.WriteString("   The keyword search is a LAST RESORT and returns tens of thousands of unrelated ads — never report an ad whose advertiser is not the competitor you are checking.\n")
	sb.WriteString("3. A cookie dialog usually appears first. Take a snapshot, click the 'only allow essential cookies' style button, then continue. You do NOT need to log in; if the page demands a login, the status is 'blocked'.\n")
	sb.WriteString("4. Snapshot the results and read the cards. Scroll to load more only while ads keep appearing.\n")
	sb.WriteString(fmt.Sprintf("5. Record at most %d ads per competitor, newest first.\n\n", competitorAdsMaxPerItem))

	sb.WriteString("## What each card gives you\n")
	sb.WriteString("- 'Hoạt động' / 'Active' or 'Không hoạt động' / 'Inactive' — the ad status.\n")
	sb.WriteString("- 'ID thư viện' / 'Library ID' — a long number. Copy it EXACTLY; it is how the next weekly run recognises the same ad.\n")
	sb.WriteString("- 'Ngày bắt đầu chạy' / start date, or a date range for finished ads. Convert to YYYY-MM-DD ('6 Tháng 6, 2026' becomes 2026-06-06).\n")
	sb.WriteString("- 'N quảng cáo sử dụng nội dung và văn bản này' — how many ads share the creative. Put N in creative_reuse_count; use 1 when the card says nothing.\n")
	sb.WriteString("- The advertiser name, the ad copy, and whether the creative is an image or a video.\n\n")

	sb.WriteString("## Hard rules\n")
	sb.WriteString(fmt.Sprintf("- Write observations and summary in language '%s'. A marketer reads this, not a developer.\n", language))
	sb.WriteString("- NEVER invent a Library ID, a date, or an ad. Everything you report must be visible on a card you actually opened.\n")
	sb.WriteString("- A competitor that runs NO ads is a real and useful finding ('no_ads'): it means they are not spending. Report it plainly.\n")
	sb.WriteString("- But NEVER report 'no_ads' for a page you could not open. If the library refused, timed out, or asked for a login, the status is 'blocked' and tool_health.browser must be 'degraded' or 'dead'.\n")
	sb.WriteString("- Ads belong ONLY to the competitor whose page you opened. If a card shows a different advertiser, it is not theirs.\n")
	sb.WriteString("- Leave a field as an empty string or empty array when the card does not show it. Guessing is worse than an empty field.\n")

	return sb.String()
}
