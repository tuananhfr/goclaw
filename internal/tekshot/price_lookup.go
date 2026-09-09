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

// price_lookup: find current list prices for ONE construction material /
// piece of equipment on Vietnamese supplier websites.
//
// Same shape as market_research: the agent may only look OUTWARD (web_search,
// web_fetch), must hand the result back through a collector tool so the
// output is validated JSON rather than prose, and a dead web tool FAILS the
// job — Drupal shows a failed job as a retryable outage, while a completed
// job is trusted as a quote candidate by an estimator.
//
// One job = one material. The caller (ERPcons erp_price) fans a whole
// material list out as N jobs so they run in parallel across workers and
// retry independently.

const (
	priceLookupFinalToolName  = "submit_price_lookup"
	priceLookupMaxIterations  = 12
	priceLookupMaxCandidates  = 5
	priceLookupDefaultResults = 3
)

func tekshotPriceLookupToolAllow() []string {
	return []string{
		"web_search",
		"web_fetch",
		"datetime",
	}
}

func (s *JobService) runPriceLookup(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	item, _ := request["item"].(map[string]any)
	if strings.TrimSpace(stringFromMap(item, "name")) == "" {
		return nil, "", fmt.Errorf("price_lookup: request.item.name is required")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewPriceLookupCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildPriceLookupPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotPriceLookupToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  priceLookupMaxIterations,
		TraceName:      "tekshot price lookup",
		TraceTags:      []string{"tekshot", "price_lookup"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	// Same guard as the research flow: a free pass that never called the
	// collector gets one forced structured turn. The pages it read are already
	// in the session, so the forced turn only re-asserts the contract.
	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit the price lookup result now by calling %s exactly once with the structured result. If nothing reliable was found, submit status 'empty' with an honest reason. Do not answer with plain text.", priceLookupFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: priceLookupFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid price lookup result")
	}

	if health, ok := report["tool_health"].(map[string]any); ok {
		if stringFromMap(health, "web_search") == "dead" {
			notes := strings.TrimSpace(stringFromMap(health, "notes"))
			if notes == "" {
				notes = "web tools were unavailable"
			}
			return nil, "", fmt.Errorf("price lookup tools unavailable: %s", notes)
		}
	}

	// Echo the caller's item back so a callback can be matched without
	// re-reading the request (Drupal keys on metadata, this is belt and braces).
	report["item"] = item

	candidates, _ := report["candidates"].([]any)
	progress := fmt.Sprintf("Price lookup completed: %d candidate(s)", len(candidates))
	if stringFromMap(report, "status") == "empty" {
		progress = "Price lookup completed: nothing reliable found"
	}
	return report, progress, nil
}

func buildPriceLookupPrompt(request map[string]any) string {
	item, _ := request["item"].(map[string]any)
	name := strings.TrimSpace(stringFromMap(item, "name"))
	spec := strings.TrimSpace(stringFromMap(item, "spec"))
	unit := strings.TrimSpace(stringFromMap(item, "unit"))
	quantity := numberFromMap(item, "quantity")
	today := strings.TrimSpace(stringFromMap(request, "today"))
	maxCandidates := int(numberFromMap(request, "max_candidates"))
	if maxCandidates <= 0 || maxCandidates > priceLookupMaxCandidates {
		maxCandidates = priceLookupDefaultResults
	}

	var sb strings.Builder
	sb.WriteString("You are a procurement assistant for a Vietnamese construction company. ")
	sb.WriteString("Find CURRENT list prices for ONE material on Vietnamese supplier websites using web_search and web_fetch, then submit ONE structured result by calling ")
	sb.WriteString(priceLookupFinalToolName)
	sb.WriteString(" exactly once. Do not return the result as plain text.\n\n")

	sb.WriteString("## Material to price\n")
	sb.WriteString("- Name: " + name + "\n")
	if spec != "" {
		sb.WriteString("- Specification: " + spec + "\n")
	}
	if unit != "" {
		sb.WriteString("- Unit: " + unit + " (the price must be per this unit)\n")
	}
	if quantity > 0 {
		sb.WriteString(fmt.Sprintf("- Quantity needed: %g\n", quantity))
	}
	if today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	sb.WriteString("\n")

	preferred := stringSliceFromMap(request, "preferred_domains")
	if len(preferred) > 0 {
		sb.WriteString("## Preferred suppliers (search these FIRST)\n")
		sb.WriteString("The estimators trust these sites. Start with `site:<domain> <material>` queries for the most relevant 2-3 of them; only widen to a general search when they yield nothing usable.\n")
		for _, domain := range preferred {
			sb.WriteString("- " + domain + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## How to work\n")
	sb.WriteString("1. Search in Vietnamese (\"<tên vật tư> giá\", \"<tên> báo giá\", \"<quy cách> giá bán\"). Prefer supplier / distributor product pages over marketplaces (Shopee, Lazada) and over blogs.\n")
	sb.WriteString("2. Open the most promising 2-4 pages with web_fetch and read the actual price on the page. A price you did not read on a fetched page does not exist.\n")
	sb.WriteString("3. The material must match: same type AND same key specification (size / capacity / rating / material). A close-but-different item is allowed only as match='similar' with the difference stated in note.\n")
	sb.WriteString(fmt.Sprintf("4. Submit up to %d candidates, best first. Fewer honest candidates beat more doubtful ones.\n", maxCandidates))
	sb.WriteString("5. If no page shows a usable price (quote-on-request, out of stock, wrong spec everywhere), submit status 'empty' with the reason. NEVER invent a price, a supplier or a URL.\n\n")

	sb.WriteString("## Per candidate\n")
	sb.WriteString("- price: integer VND per unit as shown on the page (strip thousands separators). If the page lists a range, use the lower bound and say so in note.\n")
	sb.WriteString("- vat_included: true / false when the page says so (\"đã bao gồm VAT\", \"chưa VAT\", \"giá chưa thuế\"), null when it does not say.\n")
	sb.WriteString("- delivery: 'included' when the price is delivered / \"đã bao gồm vận chuyển\", 'excluded' when \"tại kho\", \"tại nhà máy\", \"chưa gồm vận chuyển\", null when unknown.\n")
	sb.WriteString("- url: the exact page you fetched the price from. supplier: the company or the site domain. product_title: the product name exactly as on the page.\n")
	sb.WriteString("- confidence 0-100: how sure you are this is the right item at the right unit price (exact spec on a supplier page ≈ 80-95; similar item or unclear unit ≈ 40-60).\n")
	sb.WriteString("- note: Vietnamese, one short sentence: what the price covers, brand, any caveat.\n\n")

	sb.WriteString("## tool_health\n")
	sb.WriteString("Report honestly: 'ok' when web_search and web_fetch worked, 'degraded' when some calls failed, 'dead' when you could not search at all. 'dead' with status 'empty' is the CORRECT answer when tools failed.\n")

	return sb.String()
}

type PriceLookupCollectorTool struct {
	report map[string]any
}

func NewPriceLookupCollectorTool() *PriceLookupCollectorTool {
	return &PriceLookupCollectorTool{}
}

func (t *PriceLookupCollectorTool) Name() string { return priceLookupFinalToolName }

func (t *PriceLookupCollectorTool) Description() string {
	return "Submit the final price lookup result for the material as validated structured JSON. Call this once when the lookup is complete."
}

func (t *PriceLookupCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "'ok' when at least one usable price was read on a fetched page; 'empty' when honest research found none. Never fabricate a candidate to avoid 'empty'.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Required when status is 'empty': short honest reason (Vietnamese).",
			},
			"candidates": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"supplier":      map[string]any{"type": "string", "description": "Company name or site domain."},
						"url":           map[string]any{"type": "string", "description": "Exact page URL the price was read from. Must have been fetched."},
						"product_title": map[string]any{"type": "string", "description": "Product name exactly as shown on the page."},
						"price":         map[string]any{"type": "integer", "description": "Unit price in VND, integer."},
						"unit":          map[string]any{"type": "string", "description": "Unit the price applies to (cái, bộ, m, m3, kg...)."},
						"vat_included":  map[string]any{"type": []string{"boolean", "null"}, "description": "true/false when the page says; null when unknown."},
						"delivery":      map[string]any{"type": []string{"string", "null"}, "description": "'included' | 'excluded' | null."},
						"match":         map[string]any{"type": "string", "description": "'exact' when type and key spec match; 'similar' otherwise (state the difference in note)."},
						"confidence":    map[string]any{"type": "integer", "description": "0-100."},
						"note":          map[string]any{"type": "string", "description": "One short Vietnamese sentence: what the price covers, brand, caveats."},
					},
					"required": []string{"supplier", "url", "product_title", "price", "unit", "match", "confidence", "note"},
				},
			},
			"tool_health": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"web_search": map[string]any{
						"type":        "string",
						"description": "'ok' | 'degraded' | 'dead'. Be honest — 'dead' with status 'empty' is the CORRECT answer when tools failed.",
					},
					"notes": map[string]any{"type": "string", "description": "Short note on tool problems, empty string when everything worked."},
				},
				"required": []string{"web_search", "notes"},
			},
		},
		"required": []string{"status", "candidates", "tool_health"},
	}
}

func (t *PriceLookupCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validatePriceLookupReport(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured price lookup result captured.")
}

func (t *PriceLookupCollectorTool) Report() map[string]any {
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

func validatePriceLookupReport(args map[string]any) (map[string]any, error) {
	status := strings.TrimSpace(stringFromMap(args, "status"))
	if status != "ok" && status != "empty" {
		return nil, fmt.Errorf("status must be 'ok' or 'empty'")
	}
	candidates, _ := args["candidates"].([]any)

	if status == "empty" {
		if len(candidates) > 0 {
			return nil, fmt.Errorf("status 'empty' must not carry candidates")
		}
		if strings.TrimSpace(stringFromMap(args, "reason")) == "" {
			return nil, fmt.Errorf("status 'empty' requires a reason")
		}
	} else {
		if len(candidates) == 0 {
			return nil, fmt.Errorf("status 'ok' requires at least one candidate")
		}
		if len(candidates) > priceLookupMaxCandidates {
			return nil, fmt.Errorf("at most %d candidates", priceLookupMaxCandidates)
		}
		for i, raw := range candidates {
			candidate, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("candidates[%d] must be an object", i)
			}
			for _, field := range []string{"supplier", "url", "product_title", "unit", "note"} {
				if strings.TrimSpace(stringFromMap(candidate, field)) == "" {
					return nil, fmt.Errorf("candidates[%d].%s is required", i, field)
				}
			}
			if err := validateResearchSources([]any{candidate["url"]}); err != nil {
				return nil, fmt.Errorf("candidates[%d].url: %v", i, err)
			}
			if price := numberFromMap(candidate, "price"); price <= 0 || price != float64(int64(price)) {
				return nil, fmt.Errorf("candidates[%d].price must be a positive integer (VND)", i)
			}
			switch strings.TrimSpace(stringFromMap(candidate, "match")) {
			case "exact", "similar":
			default:
				return nil, fmt.Errorf("candidates[%d].match must be 'exact' or 'similar'", i)
			}
			switch strings.TrimSpace(stringFromMap(candidate, "delivery")) {
			case "", "included", "excluded":
			default:
				return nil, fmt.Errorf("candidates[%d].delivery must be 'included', 'excluded' or null", i)
			}
			if conf := numberFromMap(candidate, "confidence"); conf < 0 || conf > 100 {
				return nil, fmt.Errorf("candidates[%d].confidence must be 0-100", i)
			}
		}
	}

	health, ok := args["tool_health"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("result must contain a tool_health object")
	}
	switch strings.TrimSpace(stringFromMap(health, "web_search")) {
	case "ok", "degraded", "dead":
	default:
		return nil, fmt.Errorf("tool_health.web_search must be 'ok', 'degraded' or 'dead'")
	}

	return args, nil
}

// stringSliceFromMap reads a []string sent as JSON array; non-strings are dropped.
func stringSliceFromMap(values map[string]any, key string) []string {
	raw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
