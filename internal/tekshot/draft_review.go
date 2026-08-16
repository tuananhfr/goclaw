package tekshot

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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

// runAgentFunc abstracts ag.Run for the review loop: production wraps the
// resolved agent, tests substitute a stub that fills the ephemeral collector.
type runAgentFunc func(req agent.RunRequest) error

type draftVersion struct {
	batch  map[string]any
	lint   []LintFinding
	review *PostReviewResult
}

// runDraftReview executes the hidden quality loop AFTER a valid batch exists:
// deterministic lint → (revise) → clean-context LLM review → (revise →
// re-review) → pick the best version. It NEVER fails the job — any error
// keeps the best batch so far and records a verdict for Drupal's result_json.
func runDraftReview(run runAgentFunc, baseReq agent.RunRequest, args map[string]any, cfg reviewConfig, items []SourceItem, batch map[string]any) (map[string]any, map[string]any) {
	if len(items) != 1 {
		return batch, map[string]any{"enabled": true, "verdict": "skipped_multi_post"}
	}
	item := items[0]
	instructions := stringArg(args, "instructions")

	versions := []draftVersion{{batch: batch, lint: lintFirstPost(batch, item)}}
	lintRounds, reviewRounds := 0, 0

	// Lint stage: mechanical defects go straight to one revise, no LLM review
	// wasted on what string comparison already proved.
	if len(versions[0].lint) > 0 && cfg.MaxLintRevisions > 0 {
		if revised, ok := reviseOnce(run, baseReq, items, lintCritique(versions[0].lint)); ok {
			versions = append(versions, draftVersion{batch: revised, lint: lintFirstPost(revised, item)})
			lintRounds++
		}
	}

	// Review stage on the current best candidate.
	current := &versions[len(versions)-1]
	res, ok := reviewOnce(run, baseReq, instructions, item, cfg.Criteria, current.batch)
	if !ok {
		return current.batch, reviewMeta(versions, "review_error", lintRounds, reviewRounds)
	}
	current.review = res

	if !res.Passed && cfg.MaxReviewRevisions > 0 {
		if revised, ok := reviseOnce(run, baseReq, items, reviewCritique(res)); ok {
			v := draftVersion{batch: revised, lint: lintFirstPost(revised, item)}
			if res2, ok2 := reviewOnce(run, baseReq, instructions, item, cfg.Criteria, revised); ok2 {
				v.review = res2
			}
			versions = append(versions, v)
			reviewRounds++
		}
	}

	best := pickBestVersion(versions)
	verdict := "revise_exhausted"
	if best.review != nil && best.review.Passed {
		verdict = "pass"
	}
	return best.batch, reviewMeta(versions, verdict, lintRounds, reviewRounds)
}

func lintFirstPost(batch map[string]any, item SourceItem) []LintFinding {
	post, ok := firstPostFromBatch(batch)
	if !ok {
		return nil
	}
	return LintDraftPost(post, item)
}

// firstPostFromBatch tolerates both the collector's []map[string]any and a
// JSON round-trip's []any.
func firstPostFromBatch(batch map[string]any) (map[string]any, bool) {
	switch posts := batch["posts"].(type) {
	case []map[string]any:
		if len(posts) > 0 {
			return posts[0], true
		}
	case []any:
		if len(posts) > 0 {
			if post, ok := posts[0].(map[string]any); ok {
				return post, true
			}
		}
	}
	return nil, false
}

// reviseOnce sends the critique back into the WRITER's session (it keeps its
// research context) as one forced submit_draft_batch call. Invalid or failed
// submissions keep the previous version — a revise can only ever add a
// candidate, never lose one.
func reviseOnce(run runAgentFunc, baseReq agent.RunRequest, items []SourceItem, critique string) (map[string]any, bool) {
	collector := NewDraftBatchCollectorTool(items)
	req := baseReq
	req.RunID = uuid.NewString()
	req.Message = buildRevisePrompt(critique)
	req.EphemeralTools = []tools.Tool{collector}
	req.MaxIterations = 1
	req.ToolChoice = &providers.ToolChoice{Mode: "function", Name: finalToolName}
	if err := run(req); err != nil && collector.Batch() == nil {
		return nil, false
	}
	revised := collector.Batch()
	return revised, revised != nil
}

// reviewOnce judges one candidate in a FRESH session: the reviewer sees only
// source + post + rules, never the writer's conversation — it cannot grade
// its own homework.
func reviewOnce(run runAgentFunc, baseReq agent.RunRequest, instructions string, item SourceItem, criteria []ReviewCriterion, batch map[string]any) (*PostReviewResult, bool) {
	post, ok := firstPostFromBatch(batch)
	if !ok {
		return nil, false
	}
	collector := NewPostReviewCollectorTool(criteria, stringArg(post, "content"))
	req := baseReq
	req.SessionKey = "tekshot:review:" + uuid.NewString()
	req.RunID = uuid.NewString()
	req.Message = buildReviewPrompt(instructions, item, post, criteria)
	req.EphemeralTools = []tools.Tool{collector}
	req.MaxIterations = 1
	req.ToolChoice = &providers.ToolChoice{Mode: "function", Name: reviewToolName}
	if err := run(req); err != nil && collector.Result() == nil {
		return nil, false
	}
	result := collector.Result()
	return result, result != nil
}

// pickBestVersion: deterministic ladder — reviewed-and-passed beats anything,
// then fewer failed critical criteria (unreviewed counts as 99), then fewer
// lint findings, then the later version. A revise pass can therefore never
// make the outcome worse than the version it started from.
func pickBestVersion(versions []draftVersion) *draftVersion {
	best := &versions[0]
	for i := range versions[1:] {
		v := &versions[i+1]
		if betterVersion(v, best) {
			best = v
		}
	}
	return best
}

func betterVersion(a, b *draftVersion) bool {
	aPass, bPass := a.review != nil && a.review.Passed, b.review != nil && b.review.Passed
	if aPass != bPass {
		return aPass
	}
	aCrit, bCrit := criticalFailCount(a), criticalFailCount(b)
	if aCrit != bCrit {
		return aCrit < bCrit
	}
	if len(a.lint) != len(b.lint) {
		return len(a.lint) < len(b.lint)
	}
	return true // sau thắng khi hoà — caller duyệt theo thứ tự tạo
}

func criticalFailCount(v *draftVersion) int {
	if v.review == nil {
		return 99
	}
	return len(v.review.FailedCritical)
}

func reviewMeta(versions []draftVersion, verdict string, lintRounds, reviewRounds int) map[string]any {
	best := pickBestVersion(versions)
	entries := make([]map[string]any, 0, len(versions))
	for i := range versions {
		v := &versions[i]
		codes := make([]string, 0, len(v.lint))
		for _, f := range v.lint {
			codes = append(codes, f.Code)
		}
		entry := map[string]any{"lint": codes, "chosen": v == best}
		if v.review != nil {
			entry["review_failed"] = v.review.FailedCritical
			entry["review_passed"] = v.review.Passed
		}
		entries = append(entries, entry)
	}
	return map[string]any{
		"enabled":       true,
		"verdict":       verdict,
		"lint_rounds":   lintRounds,
		"review_rounds": reviewRounds,
		"versions":      entries,
	}
}
