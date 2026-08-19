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
	checklistFinalToolName = "submit_content_checklist"
	checklistMaxIterations = 8
	checklistMaxItems      = 30
)

// checklistRequiredFields mirrors the columns the marketing team already uses
// in its spreadsheet. Changing this list means changing the Drupal renderer,
// the editable form and the item table together.
var checklistRequiredFields = []string{"date", "content_line", "topic", "hook", "body", "usp"}

// checklistForbiddenFormatTerms are production formats this checklist does
// not support. A checklist row is always a written Facebook post accompanied
// by a static image; accepting one of these terms would send an unusable brief
// to Studio even when the prompt happened to be ignored.
var checklistForbiddenFormatTerms = []string{
	"video", "reel", "livestream", "live stream", "clip", "quay", "tiktok", "shorts",
}

// tekshotChecklistToolAllow is kept read-only. The legacy one-shot job now has
// the same internal knowledge and web research capabilities as the chat flow;
// this lets a re-run recover when Drupal's cached facts are incomplete.
func tekshotChecklistToolAllow() []string {
	return []string{
		"skill_search",
		"vault_search",
		"vault_read",
		"web_search",
		"web_fetch",
		"memory_search",
		"memory_get",
		"memory_expand",
		"knowledge_graph_search",
		"read_document",
		"read_image",
		"datetime",
	}
}

func (s *JobService) runContentChecklist(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
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

	collector := NewContentChecklistCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildContentChecklistPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotChecklistToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  checklistMaxIterations,
		TraceName:      "tekshot content checklist",
		TraceTags:      []string{"tekshot", "content_checklist"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	// Same forced-pass fallback as the draft and research flows.
	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit the final content checklist now by calling %s with every row filled in. Do not answer with plain text.", checklistFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: checklistFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid content checklist")
	}

	items, _ := report["items"].([]any)
	return report, fmt.Sprintf("Content checklist with %d rows completed", len(items)), nil
}

// ContentChecklistCollectorTool captures the one structured checklist.
type ContentChecklistCollectorTool struct {
	report map[string]any
}

// NewContentChecklistCollectorTool builds the ephemeral collector.
func NewContentChecklistCollectorTool() *ContentChecklistCollectorTool {
	return &ContentChecklistCollectorTool{}
}

// Name implements tools.Tool.
func (t *ContentChecklistCollectorTool) Name() string { return checklistFinalToolName }

// Description implements tools.Tool.
func (t *ContentChecklistCollectorTool) Description() string {
	return "Submit the final content checklist as validated structured JSON. Call this once when the plan is complete."
}

// Parameters implements tools.Tool.
func (t *ContentChecklistCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "One row per planned post, ordered by date ascending.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"date":         map[string]any{"type": "string", "description": "Publish date as YYYY-MM-DD, inside the requested planning window."},
						"time_slot":    map[string]any{"type": "string", "description": "Suggested posting time, e.g. '11:00' or '19:00-20:00'. Base it on the store's peak hours when provided."},
						"content_line": map[string]any{"type": "string", "description": "Content pillar / tuyến nội dung, e.g. 'Món chủ lực', 'Khách hàng thật', 'Ưu đãi'. Keep the set of pillars small and repeat them across the plan."},
						"topic":        map[string]any{"type": "string", "description": "The post subject in one short line. This becomes the writer's title."},
						"hook":         map[string]any{"type": "string", "description": "The opening line that stops the scroll. Concrete and specific, never a generic slogan."},
						"body":         map[string]any{"type": "string", "description": "Exactly two labelled parts: 'Nội dung:' gives 2-4 sentences of copy direction and CTA; 'Ảnh:' gives the brief for one static image. Never propose video, reel, livestream, clip or filming."},
						"usp":          map[string]any{"type": "string", "description": "Selling point and keywords to keep in the copy, comma separated."},
					},
					"required": []string{"date", "time_slot", "content_line", "topic", "hook", "body", "usp"},
				},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "2-3 sentences explaining the plan's logic: which pillars, why this mix, what it is reacting to.",
			},
		},
		"required": []string{"items", "summary"},
	}
}

// Execute implements tools.Tool.
func (t *ContentChecklistCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateContentChecklist(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured content checklist captured.")
}

// Report returns a deep copy of the captured checklist, nil when absent.
func (t *ContentChecklistCollectorTool) Report() map[string]any {
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

func validateContentChecklist(args map[string]any) (map[string]any, error) {
	items, ok := args["items"].([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("items must contain at least one row")
	}
	if len(items) > checklistMaxItems {
		return nil, fmt.Errorf("at most %d rows", checklistMaxItems)
	}
	if strings.TrimSpace(stringFromMap(args, "summary")) == "" {
		return nil, fmt.Errorf("summary is required")
	}

	for i, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d] must be an object", i)
		}
		// time_slot may legitimately be blank; the columns the marketing team
		// actually plans around may not.
		for _, field := range checklistRequiredFields {
			if strings.TrimSpace(stringFromMap(item, field)) == "" {
				return nil, fmt.Errorf("items[%d].%s is required", i, field)
			}
		}
		date := strings.TrimSpace(stringFromMap(item, "date"))
		if len(date) != 10 || date[4] != '-' || date[7] != '-' {
			return nil, fmt.Errorf("items[%d].date must be YYYY-MM-DD, got %q", i, date)
		}
		if term := checklistForbiddenFormatTerm(item); term != "" {
			return nil, fmt.Errorf("items[%d] must be a written post with a static image, not %q", i, term)
		}
	}

	return args, nil
}

func buildContentChecklistPrompt(request map[string]any) string {
	storeName := strings.TrimSpace(stringFromMap(request, "store_name"))
	pageName := strings.TrimSpace(stringFromMap(request, "page_name"))
	locality := strings.TrimSpace(stringFromMap(request, "locality"))
	language := strings.TrimSpace(stringFromMap(request, "language"))
	if language == "" {
		language = "vi"
	}
	today := strings.TrimSpace(stringFromMap(request, "today"))
	planFrom := strings.TrimSpace(stringFromMap(request, "plan_from"))
	planTo := strings.TrimSpace(stringFromMap(request, "plan_to"))
	itemCount := int(numberFromMap(request, "item_count"))
	if itemCount <= 0 {
		itemCount = 7
	}
	hasPos, _ := request["has_pos"].(bool)

	var sb strings.Builder
	sb.WriteString("You are a content planner for one Facebook page.\n")
	sb.WriteString("Build a concrete posting plan from the facts below, then submit it by calling ")
	sb.WriteString(checklistFinalToolName)
	sb.WriteString(" exactly once. Do not answer with plain text.\n\n")

	sb.WriteString("## Store context\n")
	if storeName != "" {
		sb.WriteString("- Store: " + storeName + "\n")
	}
	if pageName != "" {
		sb.WriteString("- Page: " + pageName + "\n")
	}
	if locality != "" {
		sb.WriteString("- Locality: " + locality + "\n")
	}
	if today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	if planFrom != "" && planTo != "" {
		sb.WriteString(fmt.Sprintf("- Plan window: %s to %s (every row's date must fall inside it)\n", planFrom, planTo))
	}
	sb.WriteString(fmt.Sprintf("- Rows wanted: %d\n\n", itemCount))

	writeChecklistBlock(&sb, "## Sales facts (from the store's own POS)", request, "pos_facts", hasPos)
	writeChecklistBlock(&sb, "## Reach facts (from the store's own Facebook pages)", request, "social_facts", true)
	writeChecklistBlock(&sb, "## Open alerts", request, "alerts", true)
	writeChecklistBlock(&sb, "## Market research findings", request, "research", true)
	writeChecklistBlock(&sb, "## Editorial format contract", request, "editorial_contract", true)

	sb.WriteString("## Column meaning (the team's existing spreadsheet)\n")
	sb.WriteString("- date: publish date, YYYY-MM-DD.\n")
	sb.WriteString("- Timeline is derived by Insight from date as the Vietnamese weekday. Do NOT include a timeline field in the submitted item.\n")
	sb.WriteString("- time_slot: posting time; prefer the store's real peak hours when they are given above.\n")
	sb.WriteString("- content_line: the content pillar. Use a SMALL repeating set (3-5 pillars) across the whole plan, not a new pillar per row.\n")
	sb.WriteString("- topic: the subject of the post in one line — this becomes the writer's title.\n")
	sb.WriteString("- hook: the opening line that stops the scroll. Specific and concrete.\n")
	sb.WriteString("- body: exactly two labelled parts: 'Nội dung:' (2-4 sentences of copy direction and CTA) and 'Ảnh:' (one static-image brief: photo, graphic, illustration or text design).\n")
	sb.WriteString("- usp: selling points and keywords to keep in the copy, comma separated.\n\n")

	sb.WriteString("## Hard rules\n")
	sb.WriteString(fmt.Sprintf("- Write EVERY field in language '%s'. Marketing staff read this, not developers.\n", language))
	sb.WriteString("- Every row is a written Facebook post paired with a static image. Never propose or mention video, reel, livestream, clip, filming or recording.\n")
	sb.WriteString("- A locality/address is a factual context only. It may be used when relevant, but never infer an in-person service, a visit, a filming location or an on-site activity from it.\n")
	sb.WriteString("- Reach and performance metrics are planning signals only: use them to infer which themes may interest the audience, but do NOT put dashboard figures, weekly performance recaps or metric claims in a row's topic, hook, body, image brief or keywords unless the user explicitly requests a performance/community report. A unique-viewer metric is not evidence of new viewers or new followers.\n")
	sb.WriteString("- Vault may add verified page facts, but it may never relax this editorial format contract.\n")
	sb.WriteString("- Ground the plan in the facts above. Use page, sales, timing and research facts precisely when they belong in the post; use reach facts only as planning signals unless the user explicitly requests a performance/community report. Never invent a product, service or business claim.\n")
	if pageName != "" {
		sb.WriteString(fmt.Sprintf("- This plan is for the page \"%s\" specifically. Fit that page's own audience and voice, and do not write a generic plan that would suit any of the store's other pages equally well.\n", pageName))
	}
	if hasPos {
		sb.WriteString("- Sales numbers above are real. Never invent different ones, and never claim a promotion or a price the store did not state.\n")
	} else {
		sb.WriteString("- This store has NO sales data available. Do NOT invent revenue, order counts, best sellers or price points. Plan from the page context, locality, reach, Vault and research only.\n")
	}
	sb.WriteString("- Every row must be actionable on its own: a person should be able to write the post and prepare its static image from the row alone.\n")
	sb.WriteString("- Vary hooks and angles. Repeating the same sentence pattern across rows makes the whole plan unusable.\n")
	sb.WriteString("- Spread the rows across the window instead of clustering them on one day, unless a date-bound occasion justifies it.\n")

	return sb.String()
}

// checklistForbiddenFormatTerm returns the first unsupported production term
// present in the visible copy directions of an item.
func checklistForbiddenFormatTerm(item map[string]any) string {
	for _, field := range []string{"topic", "hook", "body"} {
		value := strings.ToLower(stringFromMap(item, field))
		for _, term := range checklistForbiddenFormatTerms {
			if strings.Contains(value, term) {
				return term
			}
		}
	}
	return ""
}

// writeChecklistBlock renders a fact block, or an explicit "no data" line so
// the model can tell missing input from an empty one.
func writeChecklistBlock(sb *strings.Builder, heading string, request map[string]any, key string, enabled bool) {
	sb.WriteString(heading + "\n")
	if !enabled {
		sb.WriteString("- (not available for this store)\n\n")
		return
	}
	switch value := request[key].(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			sb.WriteString("- (no data)\n\n")
			return
		}
		sb.WriteString(value + "\n\n")
	case nil:
		sb.WriteString("- (no data)\n\n")
	default:
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil || len(encoded) == 0 {
			sb.WriteString("- (no data)\n\n")
			return
		}
		sb.WriteString(string(encoded) + "\n\n")
	}
}
