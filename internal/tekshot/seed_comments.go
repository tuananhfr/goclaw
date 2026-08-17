package tekshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	seedCommentsFinalToolName = "submit_seed_comments"
	seedCommentsMaxIterations = 6
	seedCommentsMaxItems      = 10
)

// seedCommentsToolAllow keeps the run closed-book about the open web: the job
// writes comments for a post it was handed, so browsing would only invite
// facts the post does not support. The Vault stays reachable because the
// details a first comment usually carries — full menu, prices, branch address,
// opening hours — live there and not in the caption.
func seedCommentsToolAllow() []string {
	return []string{"datetime", "vault_search", "vault_read"}
}

func (s *JobService) runSeedComments(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	content := strings.TrimSpace(stringFromMap(request, "post_content"))
	if content == "" {
		return nil, "", fmt.Errorf("seed_comments requires post_content")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewSeedCommentsCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        buildSeedCommentsPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      seedCommentsToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  seedCommentsMaxIterations,
		TraceName:      "tekshot seed comments",
		TraceTags:      []string{"tekshot", "seed_comments"},
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	// Forced pass, same shape as the draft flow. MaxIterations stays at 1:
	// ToolChoice applies on every LLM iteration, so a second iteration is a
	// chance for a later call to overwrite an already-valid submission.
	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 1
		finalReq.Message = fmt.Sprintf("Submit the seed comments now by calling %s. Do not answer with plain text.", seedCommentsFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: seedCommentsFinalToolName}
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit valid seed comments")
	}

	comments, _ := report["comments"].([]any)
	return report, fmt.Sprintf("Seed comments ready (%d)", len(comments)), nil
}

// SeedCommentsCollectorTool captures the one structured comment list.
type SeedCommentsCollectorTool struct {
	report map[string]any
}

// NewSeedCommentsCollectorTool builds the ephemeral collector.
func NewSeedCommentsCollectorTool() *SeedCommentsCollectorTool {
	return &SeedCommentsCollectorTool{}
}

// Name implements tools.Tool.
func (t *SeedCommentsCollectorTool) Name() string { return seedCommentsFinalToolName }

// Description implements tools.Tool.
func (t *SeedCommentsCollectorTool) Description() string {
	return "Submit the final seed comments for this post as validated structured JSON. Call this once."
}

// Parameters implements tools.Tool.
//
// The field descriptions below are where the writing rules live. Prompt prose
// binds far more weakly than a description attached to the slot being filled —
// the Drupal side measured a caption stuck at ~537 characters across 136 jobs
// purely because two fields carried no description.
func (t *SeedCommentsCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"comments": map[string]any{
				"type":        "array",
				"description": "The seed comments, ordered by delay_minutes ascending.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"message": map[string]any{
							"type": "string",
							"description": "A public comment posted under the post, shown under the PAGE's own name. " +
								"Write as the shop owner adding practical information a reader would want next: full menu, prices, branch address, opening hours, how to order or book. " +
								"NEVER pretend to be a customer asking about the product or praising it. " +
								"1-2 sentences. Do not repeat any sentence already in the post. " +
								"Use only facts present in the post or found in the Vault — never invent prices, addresses, promotions or opening hours. " +
								"Write in the same language as the post.",
						},
						"delay_minutes": map[string]any{
							"type": "integer",
							"description": "Minutes to wait after the post goes live before this comment is posted. " +
								"Must be a positive integer inside the allowed range given in the request, and at least the requested minimum gap after the previous comment. " +
								"Example for three comments with a 3-minute gap: 4, 11, 25.",
						},
					},
					"required": []string{"message", "delay_minutes"},
				},
			},
		},
		"required": []string{"comments"},
	}
}

// Execute implements tools.Tool.
func (t *SeedCommentsCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateSeedComments(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Seed comments captured.")
}

// Report returns the captured submission, or nil when nothing valid arrived.
func (t *SeedCommentsCollectorTool) Report() map[string]any {
	return t.report
}

func validateSeedComments(args map[string]any) (map[string]any, error) {
	raw, ok := args["comments"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("comments must be a non-empty array")
	}
	if len(raw) > seedCommentsMaxItems {
		return nil, fmt.Errorf("at most %d comments are allowed, got %d", seedCommentsMaxItems, len(raw))
	}

	out := make([]any, 0, len(raw))
	for index, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("comment %d is not an object", index+1)
		}
		message := strings.TrimSpace(stringFromMap(entry, "message"))
		if message == "" {
			return nil, fmt.Errorf("comment %d has an empty message", index+1)
		}
		delay, ok := integerNumberArg(entry, "delay_minutes")
		if !ok || delay <= 0 {
			return nil, fmt.Errorf("comment %d needs a positive integer delay_minutes", index+1)
		}
		out = append(out, map[string]any{
			"message":       message,
			"delay_minutes": delay,
		})
	}
	return map[string]any{"comments": out}, nil
}

func buildSeedCommentsPrompt(request map[string]any) string {
	var sb strings.Builder
	sb.WriteString("You are writing the first comments the Page itself will post under its own new Facebook post.\n")
	sb.WriteString("Call " + seedCommentsFinalToolName + " exactly once with the final comments. Every writing rule is in that tool's field descriptions — follow them literally.\n\n")

	if pageName := strings.TrimSpace(stringFromMap(request, "page_name")); pageName != "" {
		sb.WriteString("Page: " + pageName + "\n")
	}
	if title := strings.TrimSpace(stringFromMap(request, "post_title")); title != "" {
		sb.WriteString("Post title: " + title + "\n")
	}
	sb.WriteString("\nPost content (final, already approved — do not rewrite it):\n")
	sb.WriteString(strings.TrimSpace(stringFromMap(request, "post_content")))
	sb.WriteString("\n\n## Work order\n")
	sb.WriteString(fmt.Sprintf("- Write exactly %d comments.\n", seedCommentsCount(request)))
	sb.WriteString(fmt.Sprintf("- delay_minutes must stay between 1 and %d, with at least %d minutes between consecutive comments.\n",
		seedCommentsMaxDelay(request), seedCommentsMinGap(request)))
	sb.WriteString("- Search the Vault first when the post implies details it does not spell out (menu, prices, address, opening hours). Do not guess them.\n")
	return sb.String()
}

func seedCommentsCount(request map[string]any) int {
	if value, ok := integerNumberArg(request, "comment_count"); ok && value > 0 && value <= seedCommentsMaxItems {
		return value
	}
	return 2
}

func seedCommentsMaxDelay(request map[string]any) int {
	if value, ok := integerNumberArg(request, "max_delay_minutes"); ok && value > 0 {
		return value
	}
	return 120
}

func seedCommentsMinGap(request map[string]any) int {
	if value, ok := integerNumberArg(request, "min_gap_minutes"); ok && value > 0 {
		return value
	}
	return 3
}
