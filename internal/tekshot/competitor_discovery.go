package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	competitorFinalToolName = "submit_competitor_discovery"
	competitorMaxIterations = 12
	competitorMaxItems      = 15
)

// competitorLabels is the fixed label vocabulary shared with Drupal. Reports
// split "competitive warning" from "content idea" by this label, so adding a
// value means teaching the Drupal side what it means.
var competitorLabels = []string{"direct", "reference"}

// tekshotCompetitorToolAllow mirrors market research: this job must look
// OUTWARD at the open web. Note the contrast with content_checklist, which
// only gets datetime because it synthesises data Drupal already gathered.
func tekshotCompetitorToolAllow() []string {
	return []string{
		"web_search",
		"web_fetch",
		"datetime",
	}
}

func (s *JobService) runCompetitorDiscovery(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
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

	collector := NewCompetitorDiscoveryCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildCompetitorDiscoveryPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotCompetitorToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  competitorMaxIterations,
		TraceName:      "tekshot competitor discovery",
		TraceTags:      []string{"tekshot", "competitor_discovery"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit the competitor list now by calling %s. Do not answer with plain text.", competitorFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: competitorFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid competitor list")
	}

	// A dead web tool must FAIL the job. Drupal turns a failed job into a
	// visible alert; a completed job with an empty list would instead read as
	// "there are no competitors here", which is a very different claim.
	if health, ok := report["tool_health"].(map[string]any); ok {
		if stringFromMap(health, "web_search") == "dead" {
			notes := strings.TrimSpace(stringFromMap(health, "notes"))
			if notes == "" {
				notes = "web research tools were unavailable"
			}
			return nil, "", fmt.Errorf("research tools unavailable: %s", notes)
		}
	}

	competitors, _ := report["competitors"].([]any)
	return report, fmt.Sprintf("Competitor discovery proposed %d candidate(s)", len(competitors)), nil
}

// CompetitorDiscoveryCollectorTool captures the one structured proposal list.
type CompetitorDiscoveryCollectorTool struct {
	report map[string]any
}

// NewCompetitorDiscoveryCollectorTool builds the ephemeral collector.
func NewCompetitorDiscoveryCollectorTool() *CompetitorDiscoveryCollectorTool {
	return &CompetitorDiscoveryCollectorTool{}
}

// Name implements tools.Tool.
func (t *CompetitorDiscoveryCollectorTool) Name() string { return competitorFinalToolName }

// Description implements tools.Tool.
func (t *CompetitorDiscoveryCollectorTool) Description() string {
	return "Submit the proposed competitor list as validated structured JSON. Call this once when the search is complete."
}

// Parameters implements tools.Tool.
func (t *CompetitorDiscoveryCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"competitors": map[string]any{
				"type":        "array",
				"description": "Candidates for the store's competitor list. May be empty when the locality genuinely has none.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"name":             map[string]any{"type": "string", "description": "Business name as it is actually written locally."},
						"why_relevant":     map[string]any{"type": "string", "description": "One or two sentences: why this competes with THIS store (same food, same area, same customer)."},
						"label_suggestion": map[string]any{"type": "string", "description": "'direct' when it competes for the same customers; 'reference' when it is only worth watching for content ideas."},
						"website":          map[string]any{"type": "string", "description": "Website URL, empty string when unknown. Never guess."},
						"facebook_url":     map[string]any{"type": "string", "description": "Facebook page URL, empty string when unknown. Never guess."},
						"locality":         map[string]any{"type": "string", "description": "Where it operates, as specific as the sources allow."},
						"sources":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Real URLs actually opened via web_search/web_fetch. At least one. Never invent URLs."},
					},
					"required": []string{"name", "why_relevant", "label_suggestion", "website", "facebook_url", "locality", "sources"},
				},
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Required when the list is empty: the honest reason nothing was found. Empty string otherwise.",
			},
			"tool_health": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"web_search": map[string]any{
						"type":        "string",
						"description": "'ok' when web tools worked; 'degraded' when some calls failed; 'dead' when web research was impossible. Be honest — an empty list with 'dead' is the CORRECT answer when tools failed.",
					},
					"notes": map[string]any{"type": "string", "description": "Short note on tool problems, empty string when everything worked."},
				},
				"required": []string{"web_search", "notes"},
			},
		},
		"required": []string{"competitors", "reason", "tool_health"},
	}
}

// Execute implements tools.Tool.
func (t *CompetitorDiscoveryCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateCompetitorDiscovery(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured competitor list captured.")
}

// Report returns a deep copy of the captured list, nil when absent.
func (t *CompetitorDiscoveryCollectorTool) Report() map[string]any {
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

func validateCompetitorDiscovery(args map[string]any) (map[string]any, error) {
	competitors, ok := args["competitors"].([]any)
	if !ok {
		return nil, fmt.Errorf("competitors must be an array")
	}
	if len(competitors) > competitorMaxItems {
		return nil, fmt.Errorf("at most %d competitors", competitorMaxItems)
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

	// Unlike market research, an empty list here is genuinely common — a
	// small locality may have no comparable business — so it is accepted as
	// long as the agent says WHY instead of silently returning nothing.
	if len(competitors) == 0 && strings.TrimSpace(stringFromMap(args, "reason")) == "" {
		return nil, fmt.Errorf("an empty competitor list requires a reason")
	}

	for i, rawItem := range competitors {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("competitors[%d] must be an object", i)
		}
		for _, field := range []string{"name", "why_relevant"} {
			if strings.TrimSpace(stringFromMap(item, field)) == "" {
				return nil, fmt.Errorf("competitors[%d].%s is required", i, field)
			}
		}
		label := strings.TrimSpace(stringFromMap(item, "label_suggestion"))
		if !containsString(competitorLabels, label) {
			return nil, fmt.Errorf("competitors[%d].label_suggestion must be one of %s", i, strings.Join(competitorLabels, ", "))
		}
		if err := validateResearchSources(item["sources"]); err != nil {
			return nil, fmt.Errorf("competitors[%d]: %v", i, err)
		}
		// Optional links must still be real when present.
		for _, field := range []string{"website", "facebook_url"} {
			value := strings.TrimSpace(stringFromMap(item, field))
			if value == "" {
				continue
			}
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				return nil, fmt.Errorf("competitors[%d].%s must be an http(s) URL or an empty string", i, field)
			}
		}
	}

	return args, nil
}

func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// discoveryTarget says who counts as a rival. Without a declared goal this
// used to hardcode "same kind of food/service", which is wrong for every
// subject that is not a restaurant.
func discoveryTarget(goal string) string {
	switch goal {
	case goalRecruit:
		return "Employers competing for the SAME candidates: hiring similar roles, at a similar level, close enough that an applicant would pick one over the other."
	case goalLeads:
		return "Businesses competing for the SAME sign-ups: running comparable offers to partners, franchisees or enquirers in this niche."
	case goalBrand, goalCommunity:
		return "Pages and outlets competing for the SAME audience attention in this niche: publishers, communities and brands the audience already follows."
	default:
		return "Businesses that compete for the SAME customers: comparable products or services, close enough that a customer would choose one over the other."
	}
}

// discoveryLabels keeps direct/reference meaningful per goal — a community
// page has almost no direct rivals, and pretending otherwise fills the
// approval queue with noise.
func discoveryLabels(goal string) string {
	switch goal {
	case goalBrand, goalCommunity:
		return "Label 'reference' by default here — this subject competes for attention, not for orders. Reserve 'direct' for outlets genuinely chasing the exact same audience with the same offer."
	case goalRecruit:
		return "Label 'direct' when they hire the same roles from the same pool; 'reference' when their employer content is worth learning from but they do not compete for these candidates."
	default:
		return "Label 'direct' when they take the same customers; 'reference' when they are too far or too different to compete, but do content worth learning from."
	}
}

// discoveryReach turns the declared geography into a search strategy. The
// mode matters more than the radius: "nearby" is meaningless for a
// nationwide subject, and this is where a wrong assumption costs the whole run.
func discoveryReach(profile businessProfile) string {
	switch profile.geoMode {
	case "nationwide":
		return "This subject has NO physical catchment: search the niche nationwide and by topic, not by neighbourhood. Proximity is irrelevant — relevance to the same audience is what counts."
	case "area":
		area := profile.geoArea
		if area == "" {
			area = "the declared operating region"
		}
		return "Search across " + area + " as a whole. This subject serves a region, not a street, so a business one district away can be a real rival while a neighbour in a different trade is not."
	case "local":
		radius := profile.geoRadiusKm
		if radius <= 0 {
			radius = 3
		}
		return fmt.Sprintf("Stay within roughly %.1f km of the subject's location, its own ward/district first, then widen only if needed — a chain across the city is rarely a real competitor for a neighbourhood business.", radius)
	default:
		return "Prefer the store's own ward/district first, then widen to the surrounding district only if needed — a chain across the city is rarely a real competitor for a neighbourhood store."
	}
}

func buildCompetitorDiscoveryPrompt(request map[string]any) string {
	storeName := strings.TrimSpace(stringFromMap(request, "store_name"))
	locality := strings.TrimSpace(stringFromMap(request, "locality"))
	language := strings.TrimSpace(stringFromMap(request, "language"))
	if language == "" {
		language = "vi"
	}
	today := strings.TrimSpace(stringFromMap(request, "today"))

	var sb strings.Builder
	sb.WriteString("You are a local market analyst mapping the competitive landscape around one store.\n")
	sb.WriteString("Search the web, then submit ONE structured list by calling ")
	sb.WriteString(competitorFinalToolName)
	sb.WriteString(" exactly once. Do not answer with plain text.\n\n")

	profile := readBusinessProfile(request)

	sb.WriteString("## Store context\n")
	if subject := profile.subjectName(storeName); subject != "" {
		sb.WriteString("- Subject: " + subject + "\n")
	}
	if !profile.writeProfile(&sb) {
		sb.WriteString("- Business profile: infer only from supplied store context and verified web evidence.\n")
	}
	if locality != "" {
		sb.WriteString("- Locality: " + locality + "\n")
	}
	if today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	sb.WriteString("\n")

	// Who the store IS. Without this the agent searches "pizza + district",
	// finds the shop's own page, and proposes the store as its own rival.
	// Sibling branches of the same operator land here too — another shop of
	// the same brand is not competition.
	if own, ok := request["own_identity"].([]any); ok && len(own) > 0 {
		sb.WriteString("## This IS the business — never propose any of these\n")
		for _, raw := range own {
			if name := strings.TrimSpace(fmt.Sprintf("%v", raw)); name != "" {
				sb.WriteString("- " + name + "\n")
			}
		}
		sb.WriteString("These are the store itself, its other branches, and its own pages. Proposing one of them as a competitor is always wrong.\n\n")
	}

	// Already-known names are sent so the queue does not fill with rows the
	// user has already accepted or already rejected.
	if existing, ok := request["existing_competitors"].([]any); ok && len(existing) > 0 {
		sb.WriteString("## Already on the store's list — do NOT propose these again\n")
		for _, raw := range existing {
			if name := strings.TrimSpace(fmt.Sprintf("%v", raw)); name != "" {
				sb.WriteString("- " + name + "\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## What to find\n")
	sb.WriteString("- " + discoveryTarget(profile.goal) + "\n")
	sb.WriteString("- " + discoveryLabels(profile.goal) + "\n")
	sb.WriteString("- " + discoveryReach(profile) + "\n\n")

	sb.WriteString("## Hard rules\n")
	sb.WriteString(fmt.Sprintf("- Write why_relevant and locality in language '%s'. A marketer reads this, not a developer.\n", language))
	sb.WriteString("- Every entry MUST cite at least one real URL you actually opened. NEVER invent URLs, and NEVER guess a Facebook page — leave the field as an empty string when you did not find it.\n")
	sb.WriteString("- NEVER pad the list. A short honest list is far more useful than a long speculative one: a human reviews every row and a wrong entry poisons future research.\n")
	sb.WriteString("- NEVER propose the store itself, another branch of the same brand, or any page belonging to them. If a search result IS the store, skip it silently.\n")
	sb.WriteString("- If the area genuinely has no comparable business, return an empty list with a reason. That is a valid answer.\n")
	sb.WriteString("- If your web tools failed, say so in tool_health instead of returning an empty list with 'ok'.\n")
	sb.WriteString(fmt.Sprintf("- At most %d entries.\n", competitorMaxItems))

	return sb.String()
}
