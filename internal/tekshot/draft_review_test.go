package tekshot

import (
	"strings"
	"testing"
)

func TestReviewConfigFromArgsDisabledByDefault(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"no_block":       {},
		"nil_block":      {"review": nil},
		"not_a_map":      {"review": "yes"},
		"disabled":       {"review": map[string]any{"enabled": false}},
		"no_criteria":    {"review": map[string]any{"enabled": true}},
		"empty_criteria": {"review": map[string]any{"enabled": true, "criteria": []any{}}},
	} {
		if cfg := reviewConfigFromArgs(args); cfg.Enabled {
			t.Fatalf("%s: expected disabled config", name)
		}
	}
}

func TestReviewConfigFromArgsParsesFullBlock(t *testing.T) {
	args := map[string]any{"review": map[string]any{
		"enabled":              true,
		"max_lint_revisions":   float64(2),
		"max_review_revisions": float64(0),
		"criteria": []any{
			map[string]any{"id": "brief_coverage", "description": "Mọi ý brief được triển khai.", "critical": true},
			map[string]any{"id": "cta_quality", "description": "CTA rõ.", "critical": false},
		},
	}}
	cfg := reviewConfigFromArgs(args)
	if !cfg.Enabled || cfg.MaxLintRevisions != 2 || cfg.MaxReviewRevisions != 0 {
		t.Fatalf("bad config: %+v", cfg)
	}
	if len(cfg.Criteria) != 2 || !cfg.Criteria[0].Critical || cfg.Criteria[1].Critical {
		t.Fatalf("bad criteria: %+v", cfg.Criteria)
	}
}

func TestReviewConfigFromArgsClampsCaps(t *testing.T) {
	args := map[string]any{"review": map[string]any{
		"enabled":              true,
		"max_lint_revisions":   float64(9),
		"max_review_revisions": float64(-3),
		"criteria":             []any{map[string]any{"id": "x", "description": "d", "critical": true}},
	}}
	cfg := reviewConfigFromArgs(args)
	if cfg.MaxLintRevisions != 2 || cfg.MaxReviewRevisions != 0 {
		t.Fatalf("caps must clamp to [0,2], got %+v", cfg)
	}
}

func TestBuildReviewPromptCarriesWriterInstructionsAndCriteria(t *testing.T) {
	instructions := "## Content Writing Guidelines\nLuật fold 3 dòng đầu..."
	item := SourceItem{SourceIndex: 1, ChecklistItem: "Row gốc", SourceTitle: "Tiêu đề", SourceBrief: "Brief gốc"}
	post := map[string]any{"title": "Tiêu đề", "content": "Caption đã viết.", "hashtags": "#a #b #c"}
	criteria := []ReviewCriterion{{ID: "brief_coverage", Description: "Mọi ý brief.", Critical: true}}

	prompt := buildReviewPrompt(instructions, item, post, criteria)
	for _, want := range []string{
		instructions,       // một nguồn chuẩn: reviewer nhận đúng instructions của writer
		"brief_coverage",   // id tiêu chí
		"Caption đã viết.", // content bị chấm
		"Brief gốc",        // source đối chiếu
		reviewToolName,     // contract đầu ra
		"verbatim",         // luật evidence
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("review prompt missing %q", want)
		}
	}
	if !strings.Contains(prompt, "must NOT rewrite") {
		t.Fatal("reviewer must be explicitly forbidden from rewriting")
	}
}

func TestCritiqueBuilders(t *testing.T) {
	// Critique builders chỉ dựng mảnh nhận xét; câu "gọi lại tool nào" thuộc về
	// buildRevisePrompt và cố ý chỉ xuất hiện một lần trong prompt cuối.
	lint := lintCritique([]LintFinding{{Code: "title_echo", Message: "First line repeats the title."}})
	if !strings.Contains(lint, "title_echo") {
		t.Fatalf("lint critique must name findings: %q", lint)
	}
	if !strings.Contains(buildRevisePrompt(lint), finalToolName) {
		t.Fatal("revise prompt must name the resubmission tool")
	}
	review := reviewCritique(&PostReviewResult{
		Passed:         false,
		FailedCritical: []string{"brief_coverage"},
		RevisionNotes:  "Bổ sung ý bảo hành.",
		Criteria: []CriterionVerdict{{
			ID: "brief_coverage", Passed: false,
			Evidence: "Câu bị lỗi trong caption.", Note: "Thiếu ý bảo hành.",
		}},
	})
	for _, want := range []string{"brief_coverage", "Câu bị lỗi trong caption.", "Bổ sung ý bảo hành."} {
		if !strings.Contains(review, want) {
			t.Fatalf("review critique missing %q", want)
		}
	}
	if !strings.Contains(buildRevisePrompt(review), finalToolName) {
		t.Fatal("revise prompt must name the resubmission tool")
	}
}
