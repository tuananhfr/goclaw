package tekshot

import (
	"fmt"
	"strings"
)

// reviewConfig mirrors the `review` block Drupal ships in the job request.
// Absent block, enabled=false, or an empty criteria list all mean the loop is
// off and the draft flow behaves exactly as before — that invariant is what
// makes this feature safe to deploy dark.
type reviewConfig struct {
	Enabled            bool
	MaxLintRevisions   int
	MaxReviewRevisions int
	Criteria           []ReviewCriterion
}

func reviewConfigFromArgs(args map[string]any) reviewConfig {
	block, ok := args["review"].(map[string]any)
	if !ok {
		return reviewConfig{}
	}
	enabled, _ := block["enabled"].(bool)
	if !enabled {
		return reviewConfig{}
	}

	var criteria []ReviewCriterion
	if raw, ok := block["criteria"].([]any); ok {
		for _, entry := range raw {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(stringArg(m, "id"))
			if id == "" {
				continue
			}
			critical, _ := m["critical"].(bool)
			criteria = append(criteria, ReviewCriterion{
				ID:          id,
				Description: strings.TrimSpace(stringArg(m, "description")),
				Critical:    critical,
			})
		}
	}
	if len(criteria) == 0 {
		return reviewConfig{}
	}

	return reviewConfig{
		Enabled:            true,
		MaxLintRevisions:   clampRevisions(block, "max_lint_revisions"),
		MaxReviewRevisions: clampRevisions(block, "max_review_revisions"),
		Criteria:           criteria,
	}
}

func clampRevisions(block map[string]any, key string) int {
	value, ok := block[key].(float64)
	if !ok {
		return 1
	}
	n := int(value)
	if n < 0 {
		return 0
	}
	if n > 2 {
		return 2
	}
	return n
}

// buildReviewPrompt assembles the reviewer's one-shot prompt. The writer's
// instructions ride along VERBATIM — single source of writing rules; the
// reviewer only adds process (judge, quote, do not rewrite).
func buildReviewPrompt(instructions string, item SourceItem, post map[string]any, criteria []ReviewCriterion) string {
	var sb strings.Builder
	sb.WriteString("You are Tekshot Studio's content reviewer. Judge ONE finished draft post against the writing rules below. You must NOT rewrite the content — your only output is a structured review.\n\n")
	sb.WriteString("=== WRITING RULES THE WRITER RECEIVED (judge against these) ===\n")
	sb.WriteString(instructions)
	sb.WriteString("\n=== END WRITING RULES ===\n\n")

	sb.WriteString("SOURCE CHECKLIST ITEM (ground truth — the post must develop this, nothing beyond it):\n")
	sb.WriteString(fmt.Sprintf("- source_index: %d\n- checklist_item: %s\n- source_title: %s\n- source_brief: %s\n", item.SourceIndex, item.ChecklistItem, item.SourceTitle, item.SourceBrief))
	if strings.TrimSpace(item.SourceText) != "" {
		sb.WriteString("- source_text: " + item.SourceText + "\n")
	}

	sb.WriteString("\nTHE POST UNDER REVIEW:\n")
	for _, key := range []string{"title", "brief", "content", "hashtags", "publish_at"} {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", key, stringArg(post, key)))
	}

	sb.WriteString("\nCRITERIA (evaluate every one, in this order):\n")
	for _, c := range criteria {
		marker := "non-critical"
		if c.Critical {
			marker = "CRITICAL"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", marker, c.ID, c.Description))
	}

	sb.WriteString("\nOUTPUT CONTRACT:\n")
	sb.WriteString(fmt.Sprintf("- Call %s exactly once with one entry per criterion id above.\n", reviewToolName))
	sb.WriteString("- A fail verdict REQUIRES evidence: a verbatim quote copied from the content field, character-for-character. No quote, no fail.\n")
	sb.WriteString("- Do not invent problems. When the post genuinely satisfies a criterion, pass it.\n")
	sb.WriteString("- When any criterion fails, write revision_notes as concrete instructions the writer can act on.\n")
	return sb.String()
}

func buildRevisePrompt(critique string) string {
	var sb strings.Builder
	sb.WriteString("Your submitted draft did not pass review. Revise it and resubmit.\n\n")
	sb.WriteString(critique)
	sb.WriteString(fmt.Sprintf("\nRevise ONLY what the critique calls out — keep everything that already works. title, brief and publish fields stay unchanged. Then call %s exactly once with the complete revised post.\n", finalToolName))
	return sb.String()
}

func lintCritique(findings []LintFinding) string {
	var sb strings.Builder
	sb.WriteString("Automatic checks found these defects:\n")
	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", f.Code, f.Message))
	}
	return sb.String()
}

func reviewCritique(res *PostReviewResult) string {
	var sb strings.Builder
	sb.WriteString("The reviewer failed these criteria:\n")
	for _, c := range res.Criteria {
		if c.Passed {
			continue
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n  Offending passage: %q\n", c.ID, c.Note, c.Evidence))
	}
	sb.WriteString("\nReviewer's instructions: " + res.RevisionNotes + "\n")
	return sb.String()
}
