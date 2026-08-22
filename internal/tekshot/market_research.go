package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	researchFinalToolName    = "submit_market_research"
	researchMaxIterations    = 10
	researchMaxItemsPerBlock = 10
)

// researchSectionKeys is the fixed section contract shared with Drupal
// (tekshot_insight). Adding a section means updating the Drupal renderer too.
var researchSectionKeys = []string{"trends", "seasonal_hooks", "local_signals"}

// tekshotResearchToolAllow is deliberately narrower than the draft flow's
// allow-list: market research must only look OUTWARD (web), never into the
// studio vault/memory of another concern.
func tekshotResearchToolAllow() []string {
	return []string{
		"web_search",
		"web_fetch",
		"datetime",
	}
}

func (s *JobService) runMarketResearch(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewMarketResearchCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildMarketResearchPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotResearchToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  researchMaxIterations,
		TraceName:      "tekshot market research",
		TraceTags:      []string{"tekshot", "market_research"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	// Mirror the draft flow: if the free pass never called the collector,
	// force one final structured submission. The research itself is already in
	// the session history, so the forced turn only re-asserts intent.
	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit the final market research report now by calling %s with the complete structured result. Do not answer with plain text.", researchFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: researchFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid market research report")
	}

	// A dead web tool must FAIL the job, never complete with a hollow report —
	// Drupal treats a failed job as an alertable tool outage, while a completed
	// job is trusted content shown to marketing users.
	if health, ok := report["tool_health"].(map[string]any); ok {
		if stringFromMap(health, "web_search") == "dead" {
			notes := strings.TrimSpace(stringFromMap(health, "notes"))
			if notes == "" {
				notes = "web research tools were unavailable"
			}
			return nil, "", fmt.Errorf("research tools unavailable: %s", notes)
		}
	}

	return report, "Market research completed", nil
}

type MarketResearchCollectorTool struct {
	report map[string]any
}

func NewMarketResearchCollectorTool() *MarketResearchCollectorTool {
	return &MarketResearchCollectorTool{}
}

func (t *MarketResearchCollectorTool) Name() string { return researchFinalToolName }

func (t *MarketResearchCollectorTool) Description() string {
	return "Submit the final market research report as validated structured JSON. Call this once when the research is complete."
}

func (t *MarketResearchCollectorTool) Parameters() map[string]any {
	sectionSchema := func(description string) map[string]any {
		return map[string]any{
			"type":                 "object",
			"description":          description,
			"additionalProperties": false,
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"description": "'ok' when items were found; 'empty' when honest research found nothing. Never fabricate items to avoid 'empty'.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Required when status is 'empty': short honest reason why nothing was found.",
				},
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"title":            map[string]any{"type": "string"},
							"summary":          map[string]any{"type": "string", "description": "2-3 sentences, written in the requested output language."},
							"why_it_matters":   map[string]any{"type": "string", "description": "Why this matters for THIS store (business context + locality)."},
							"suggested_action": map[string]any{"type": "string", "description": "Concrete next step for the marketing team (post idea, campaign, timing)."},
							"sources":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Real URLs actually read via web_search/web_fetch. At least one. Never invent URLs."},
						},
						"required": []string{"title", "summary", "why_it_matters", "suggested_action", "sources"},
					},
				},
			},
			"required": []string{"status", "items"},
		}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"sections": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"trends":         sectionSchema("Content/product trends relevant to the store's market and current context."),
					"seasonal_hooks": sectionSchema("Upcoming holidays, occasions and seasonal angles within the horizon."),
					"local_signals":  sectionSchema("Local market signals around the store's locality: openings, closures, events."),
				},
				"required": []string{"trends", "seasonal_hooks", "local_signals"},
			},
			"tool_health": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"web_search": map[string]any{
						"type":        "string",
						"description": "'ok' when web tools worked; 'degraded' when some calls failed; 'dead' when web research was impossible. Be honest — 'dead' with empty sections is the CORRECT answer when tools failed.",
					},
					"notes": map[string]any{"type": "string", "description": "Short note on tool problems, empty string when everything worked."},
				},
				"required": []string{"web_search", "notes"},
			},
		},
		"required": []string{"sections", "tool_health"},
	}
}

func (t *MarketResearchCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateMarketResearchReport(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured market research report captured.")
}

func (t *MarketResearchCollectorTool) Report() map[string]any {
	if t.report == nil {
		return nil
	}
	// JSON round-trip = cheap deep copy; the report is small by contract.
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

func validateMarketResearchReport(args map[string]any) (map[string]any, error) {
	rawSections, ok := args["sections"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("report must contain a sections object")
	}

	emptyCount := 0
	for _, key := range researchSectionKeys {
		section, ok := rawSections[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sections.%s is required", key)
		}
		status := strings.TrimSpace(stringFromMap(section, "status"))
		if status != "ok" && status != "empty" {
			return nil, fmt.Errorf("sections.%s.status must be 'ok' or 'empty'", key)
		}
		items, _ := section["items"].([]any)
		if status == "empty" {
			emptyCount++
			if len(items) > 0 {
				return nil, fmt.Errorf("sections.%s: status 'empty' must not carry items", key)
			}
			if strings.TrimSpace(stringFromMap(section, "reason")) == "" {
				return nil, fmt.Errorf("sections.%s: status 'empty' requires a reason", key)
			}
			continue
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("sections.%s: status 'ok' requires at least one item", key)
		}
		if len(items) > researchMaxItemsPerBlock {
			return nil, fmt.Errorf("sections.%s: at most %d items", key, researchMaxItemsPerBlock)
		}
		for i, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("sections.%s.items[%d] must be an object", key, i)
			}
			for _, field := range []string{"title", "summary", "why_it_matters", "suggested_action"} {
				if strings.TrimSpace(stringFromMap(item, field)) == "" {
					return nil, fmt.Errorf("sections.%s.items[%d].%s is required", key, i, field)
				}
			}
			if err := validateResearchSources(item["sources"]); err != nil {
				return nil, fmt.Errorf("sections.%s.items[%d]: %v", key, i, err)
			}
		}
	}

	health, ok := args["tool_health"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("report must contain a tool_health object")
	}
	webHealth := strings.TrimSpace(stringFromMap(health, "web_search"))
	switch webHealth {
	case "ok", "degraded", "dead":
	default:
		return nil, fmt.Errorf("tool_health.web_search must be 'ok', 'degraded' or 'dead'")
	}

	// The "silent empty report" guard: a report where EVERY section is empty
	// while tools are claimed healthy is almost always a lazy or failed run
	// (seasonal hooks alone are rarely empty for a real business). Force
	// the agent to either research harder or admit the tool failure.
	if emptyCount == len(researchSectionKeys) && webHealth == "ok" {
		return nil, fmt.Errorf("all sections are empty while tool_health.web_search is 'ok' — either research produced findings (seasonal hooks rarely have none) or web tools failed and web_search must be 'degraded' or 'dead'")
	}

	return args, nil
}

func validateResearchSources(raw any) error {
	sources, ok := raw.([]any)
	if !ok || len(sources) == 0 {
		return fmt.Errorf("sources must contain at least one URL")
	}
	for i, rawSource := range sources {
		source, ok := rawSource.(string)
		if !ok {
			return fmt.Errorf("sources[%d] must be a string URL", i)
		}
		trimmed := strings.TrimSpace(source)
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return fmt.Errorf("sources[%d] must be an http(s) URL", i)
		}
	}
	return nil
}

// hasKnownCompetitors reports whether Drupal sent an approved competitor list.
func hasKnownCompetitors(request map[string]any) bool {
	competitors, ok := request["competitors"].([]any)
	return ok && len(competitors) > 0
}

// writeKnownCompetitors renders the approved competitor list into the prompt.
//
// Only APPROVED rows ever reach here — Drupal filters them — so the agent can
// treat the list as vetted and go look at these businesses specifically
// instead of guessing who matters in the area.
func writeKnownCompetitors(sb *strings.Builder, request map[string]any) {
	if !hasKnownCompetitors(request) {
		return
	}
	competitors, _ := request["competitors"].([]any)

	sb.WriteString("## Competitors the store's team has confirmed\n")
	for _, raw := range competitors {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringFromMap(item, "name"))
		if name == "" {
			continue
		}
		var line strings.Builder
		line.WriteString("- " + name)
		if label := strings.TrimSpace(stringFromMap(item, "label")); label == "reference" {
			line.WriteString(" (watch for content ideas only)")
		} else {
			line.WriteString(" (direct competitor)")
		}
		for _, key := range []string{"website", "facebook_url"} {
			if link := strings.TrimSpace(stringFromMap(item, key)); link != "" {
				line.WriteString(" " + link)
			}
		}
		sb.WriteString(line.String() + "\n")
	}
	sb.WriteString("\n")
}

// researchTrendsFocus points the trends section at what the subject actually
// does. A recruiting page researching food trends is wasted budget.
func researchTrendsFocus(goal string) string {
	switch goal {
	case goalRecruit:
		return "what is currently moving in the Vietnamese labour market for these roles: pay levels, hiring seasons, benefits candidates ask for, employer-branding content that works."
	case goalLeads:
		return "what is currently working to collect leads in this niche in Vietnam: offers, lead magnets, campaign formats, and what partners/franchisees are asking about."
	case goalBrand, goalCommunity:
		return "what content is currently gaining traction in this niche in Vietnam: formats, angles, recurring topics and the discussions the audience is having."
	default:
		return "what is currently trending for this subject's declared business in Vietnam: products, content formats, campaign styles."
	}
}

// researchLocalSignalsFocus keeps the third section meaningful for subjects
// that have no catchment at all — "news near the shop" is nonsense for a
// nationwide page.
func researchLocalSignalsFocus(profile businessProfile) string {
	switch profile.geoMode {
	case "nationwide":
		return "signals across the whole niche rather than one neighbourhood: notable entrants, closures, national campaigns and policy or platform changes that affect this subject. There is no catchment to survey — do not invent one."
	case "area":
		return "signals across the declared operating region: notable openings/closures, sector events, trade fairs, infrastructure or policy changes. Cover the region, not a single street."
	default:
		return "news around the subject's locality that affects foot traffic or competition: notable openings/closures, local events, festivals, road works. If the locality is too small to yield news, widen to its district/city and say so in the summary."
	}
}

func buildMarketResearchPrompt(request map[string]any) string {
	locality := strings.TrimSpace(stringFromMap(request, "locality"))
	storeName := strings.TrimSpace(stringFromMap(request, "store_name"))
	language := strings.TrimSpace(stringFromMap(request, "language"))
	if language == "" {
		language = "vi"
	}
	today := strings.TrimSpace(stringFromMap(request, "today"))
	horizonWeeks := int(numberFromMap(request, "horizon_weeks"))
	if horizonWeeks <= 0 {
		horizonWeeks = 4
	}

	var sb strings.Builder
	sb.WriteString("You are a market research analyst working for a local store's marketing team.\n")
	sb.WriteString("Research the CURRENT market using web_search and web_fetch, then submit ONE structured report by calling ")
	sb.WriteString(researchFinalToolName)
	sb.WriteString(" exactly once. Do not return the report as plain text.\n\n")

	profile := readBusinessProfile(request)

	sb.WriteString("## Store context\n")
	if subject := profile.subjectName(storeName); subject != "" {
		sb.WriteString("- Subject: " + subject + "\n")
	}
	// A declared profile replaces the guesswork. Without one the agent only
	// has the store name, which is often an internal category rather than a
	// business — hence the fallback wording, not a removal.
	if !profile.writeProfile(&sb) {
		sb.WriteString("- Business profile: derive only from the supplied store context and verified web evidence.\n")
	}
	if locality != "" {
		sb.WriteString("- Locality: " + locality + "\n")
	}
	if today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	sb.WriteString(fmt.Sprintf("- Planning horizon: next %d weeks\n\n", horizonWeeks))

	writeKnownCompetitors(&sb, request)

	sb.WriteString("## What to research (three sections, all required)\n")
	sb.WriteString("1. trends — " + researchTrendsFocus(profile.goal) + " Prefer signals from the last 60 days.\n")
	sb.WriteString(fmt.Sprintf("2. seasonal_hooks — holidays, occasions and seasonal angles inside the next %d weeks that this subject can build campaigns around, with enough lead time to prepare content BEFORE the date.\n", horizonWeeks))
	sb.WriteString("3. local_signals — " + researchLocalSignalsFocus(profile) + "\n\n")

	sb.WriteString("## Hard rules\n")
	sb.WriteString(fmt.Sprintf("- Write ALL user-facing text (title, summary, why_it_matters, suggested_action) in language '%s'. It is read by marketing staff, not developers.\n", language))
	sb.WriteString("- Every item MUST cite at least one real URL you actually opened via web_search/web_fetch. NEVER invent or guess URLs. Prefer Vietnamese sources for Vietnamese topics.\n")
	sb.WriteString("- NEVER fabricate findings. A section with nothing genuinely found gets status 'empty' plus an honest reason — that is a valid, respected answer.\n")
	sb.WriteString("- Distinguish 'nothing in the market' from 'my tools failed': if web_search/web_fetch calls errored, say so in tool_health (web_search: 'degraded' or 'dead') instead of returning empty sections with 'ok'.\n")
	sb.WriteString("- suggested_action must be concrete enough for a marketer to act on this week (a post idea, a campaign angle, a timing recommendation) — not generic advice.\n")
	sb.WriteString(fmt.Sprintf("- 3 to %d items per section. Quality over quantity.\n", researchMaxItemsPerBlock))
	if hasKnownCompetitors(request) {
		sb.WriteString("- The competitor list above was approved by the store's own team: local_signals MUST check what those specific businesses are doing right now (new items, promotions, openings, closures) instead of describing the area in general.\n")
	}

	return sb.String()
}
