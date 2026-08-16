package tekshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const reviewToolName = "submit_post_review"

// ReviewCriterion is one rubric item shipped by Drupal in the job request.
// Descriptions are short pointers back to the writer instructions — the full
// writing norms live there so writer and reviewer can never disagree.
type ReviewCriterion struct {
	ID          string
	Description string
	Critical    bool
}

type CriterionVerdict struct {
	ID       string
	Passed   bool
	Evidence string
	Note     string
}

// PostReviewResult is the validated review. Passed is DERIVED in Go from the
// critical criteria — the model's overall verdict is accepted as input for
// clarity but never trusted.
type PostReviewResult struct {
	Passed         bool
	Criteria       []CriterionVerdict
	RevisionNotes  string
	FailedCritical []string
}

// PostReviewCollectorTool is the forced final tool of a review call, mirroring
// DraftBatchCollectorTool: it validates and captures, it does not chat.
type PostReviewCollectorTool struct {
	criteria    []ReviewCriterion
	normContent string
	result      *PostReviewResult
}

func NewPostReviewCollectorTool(criteria []ReviewCriterion, content string) *PostReviewCollectorTool {
	return &PostReviewCollectorTool{criteria: criteria, normContent: normalizeForLint(content)}
}

func (t *PostReviewCollectorTool) Name() string { return reviewToolName }

func (t *PostReviewCollectorTool) Description() string {
	return "Submit the final structured review of one Tekshot draft post. Call this exactly once."
}

func (t *PostReviewCollectorTool) Parameters() map[string]any {
	ids := make([]string, 0, len(t.criteria))
	for _, c := range t.criteria {
		ids = append(ids, c.ID)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type": "string", "enum": []string{"pass", "revise"},
				"description": "Overall verdict. Must be revise when any critical criterion fails.",
			},
			"criteria": map[string]any{
				"type":        "array",
				"description": fmt.Sprintf("One entry per criterion id, exactly these ids once each: %s", strings.Join(ids, ", ")),
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"verdict":  map[string]any{"type": "string", "enum": []string{"pass", "fail"}},
						"evidence": map[string]any{"type": "string", "description": "REQUIRED on fail: a verbatim quote from the content proving the failure. Must appear in the content exactly."},
						"note":     map[string]any{"type": "string", "description": "Short reason, one sentence."},
					},
					"required": []string{"id", "verdict", "evidence", "note"},
				},
			},
			"revision_notes": map[string]any{
				"type":        "string",
				"description": "Actionable instructions for the writer. Required when any criterion fails.",
			},
		},
		"required":             []string{"verdict", "criteria", "revision_notes"},
		"additionalProperties": false,
	}
}

func (t *PostReviewCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	result, err := t.validate(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.result = result
	return tools.SilentResult("Structured post review captured.")
}

func (t *PostReviewCollectorTool) Result() *PostReviewResult {
	return t.result
}

func (t *PostReviewCollectorTool) validate(args map[string]any) (*PostReviewResult, error) {
	required := map[string]bool{"verdict": true, "criteria": true, "revision_notes": true}
	if !sameKeys(args, required) {
		return nil, fmt.Errorf("review must contain exactly verdict, criteria, and revision_notes")
	}

	rawList, ok := args["criteria"].([]any)
	if !ok {
		return nil, fmt.Errorf("criteria must be an array")
	}

	expected := make(map[string]ReviewCriterion, len(t.criteria))
	for _, c := range t.criteria {
		expected[c.ID] = c
	}

	seen := map[string]bool{}
	verdicts := make([]CriterionVerdict, 0, len(rawList))
	var failedCritical []string
	anyFail := false

	for i, raw := range rawList {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("criteria[%d] must be an object", i)
		}
		id := strings.TrimSpace(stringArg(entry, "id"))
		criterion, known := expected[id]
		if !known {
			return nil, fmt.Errorf("criteria[%d] has unknown id %q", i, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("criterion %q appears more than once", id)
		}
		seen[id] = true

		verdict := strings.TrimSpace(stringArg(entry, "verdict"))
		evidence := strings.TrimSpace(stringArg(entry, "evidence"))
		switch verdict {
		case "pass":
		case "fail":
			anyFail = true
			if evidence == "" {
				return nil, fmt.Errorf("criterion %q failed without evidence; quote the offending content verbatim", id)
			}
			if !strings.Contains(t.normContent, normalizeForLint(evidence)) {
				return nil, fmt.Errorf("criterion %q evidence is not a verbatim quote from the content", id)
			}
			if criterion.Critical {
				failedCritical = append(failedCritical, id)
			}
		default:
			return nil, fmt.Errorf("criterion %q verdict must be pass or fail", id)
		}
		verdicts = append(verdicts, CriterionVerdict{
			ID: id, Passed: verdict == "pass", Evidence: evidence,
			Note: strings.TrimSpace(stringArg(entry, "note")),
		})
	}

	for id := range expected {
		if !seen[id] {
			return nil, fmt.Errorf("criterion %q is missing from the review", id)
		}
	}

	notes := strings.TrimSpace(stringArg(args, "revision_notes"))
	if anyFail && notes == "" {
		return nil, fmt.Errorf("revision_notes is required when any criterion fails")
	}

	return &PostReviewResult{
		Passed:         len(failedCritical) == 0,
		Criteria:       verdicts,
		RevisionNotes:  notes,
		FailedCritical: failedCritical,
	}, nil
}
