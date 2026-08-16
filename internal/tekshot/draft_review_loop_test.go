package tekshot

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

func loopTestItems() []SourceItem {
	return []SourceItem{{SourceIndex: 1, ChecklistItem: "row", SourceTitle: "Tiêu đề dài đủ tám ký tự", SourceBrief: "Brief."}}
}

func loopTestBatch(content string) map[string]any {
	return map[string]any{
		"title": "Batch", "summary": "s",
		"posts": []map[string]any{{
			"title": "Tiêu đề dài đủ tám ký tự", "brief": "Brief.", "pillar": "p",
			"content": content, "hashtags": "#a #b #c",
			"publish_at": "2026-08-20T10:00:00", "publish_date": "2026-08-20",
			"publish_time": "10:00", "checklist_item": "row", "source_index": 1,
		}},
	}
}

// loopTestArgs is the JSON-decoded shape the collector VALIDATES (posts as
// []any), whereas loopTestBatch mirrors what Batch() RETURNS after validation
// (posts as []map[string]any). The two shapes are not interchangeable.
func loopTestArgs(content string) map[string]any {
	args := loopTestBatch(content)
	posts := args["posts"].([]map[string]any)
	raw := make([]any, 0, len(posts))
	for _, p := range posts {
		raw = append(raw, any(p))
	}
	args["posts"] = raw
	return args
}

func loopTestConfig() reviewConfig {
	return reviewConfig{
		Enabled: true, MaxLintRevisions: 1, MaxReviewRevisions: 1,
		Criteria: []ReviewCriterion{{ID: "brief_coverage", Description: "d", Critical: true}},
	}
}

const cleanContent = "Caption đủ dài và sạch sẽ, phát triển brief thành nội dung thật với lợi ích cụ thể, số liệu rõ ràng và một lời mời hành động tự nhiên ở cuối để người đọc nhắn tin ngay hôm nay."

// stubRun bơm kết quả canned vào collector của từng lượt forced-tool.
func stubRun(t *testing.T, reviewVerdicts []string, revisedContents []string) runAgentFunc {
	t.Helper()
	reviewCall, reviseCall := 0, 0
	return func(req agent.RunRequest) error {
		if req.ToolChoice == nil || req.MaxIterations != 1 {
			t.Fatal("every review-loop call must be forced-tool with MaxIterations=1")
		}
		tool := req.EphemeralTools[0]
		switch req.ToolChoice.Name {
		case reviewToolName:
			verdict := reviewVerdicts[reviewCall]
			reviewCall++
			args := map[string]any{
				"verdict": "pass",
				"criteria": []any{map[string]any{
					"id": "brief_coverage", "verdict": verdict,
					"evidence": map[string]string{"pass": "", "fail": "Caption"}[verdict],
					"note":     "n",
				}},
				"revision_notes": map[string]string{"pass": "", "fail": "Sửa lại."}[verdict],
			}
			if res := tool.Execute(context.Background(), args); res.IsError {
				t.Fatalf("stub review rejected: %s", res.ForLLM)
			}
		case finalToolName:
			content := revisedContents[reviseCall]
			reviseCall++
			if res := tool.Execute(context.Background(), loopTestArgs(content)); res.IsError {
				t.Fatalf("stub revise rejected: %s", res.ForLLM)
			}
		default:
			t.Fatalf("unexpected forced tool %q", req.ToolChoice.Name)
		}
		return nil
	}
}

func TestRunDraftReviewCleanPostPassesFirstReview(t *testing.T) {
	run := stubRun(t, []string{"pass"}, nil)
	batch, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), loopTestItems(), loopTestBatch(cleanContent))
	if meta["verdict"] != "pass" {
		t.Fatalf("expected pass, got %v", meta["verdict"])
	}
	if batch["posts"].([]map[string]any)[0]["content"] != cleanContent {
		t.Fatal("clean post must come back untouched")
	}
}

func TestRunDraftReviewLintFailureTriggersReviseBeforeReview(t *testing.T) {
	// Bản đầu dính title_echo → 1 revise (lint) → bản sạch → review pass.
	dirty := "Tiêu đề dài đủ tám ký tự\n" + cleanContent
	run := stubRun(t, []string{"pass"}, []string{cleanContent})
	batch, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), loopTestItems(), loopTestBatch(dirty))
	if meta["lint_rounds"] != 1 || meta["verdict"] != "pass" {
		t.Fatalf("expected 1 lint round then pass, got %+v", meta)
	}
	if batch["posts"].([]map[string]any)[0]["content"] != cleanContent {
		t.Fatal("revised content must be returned")
	}
}

func TestRunDraftReviewReviewFailureRevisesThenRereviews(t *testing.T) {
	run := stubRun(t, []string{"fail", "pass"}, []string{cleanContent + " Bản đã sửa."})
	batch, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), loopTestItems(), loopTestBatch(cleanContent))
	if meta["review_rounds"] != 1 || meta["verdict"] != "pass" {
		t.Fatalf("expected 1 review round then pass, got %+v", meta)
	}
	if batch["posts"].([]map[string]any)[0]["content"] == cleanContent {
		t.Fatal("expected the revised version to be chosen")
	}
}

func TestRunDraftReviewExhaustedKeepsBestAndFlags(t *testing.T) {
	// Fail cả hai lượt review → verdict revise_exhausted, batch vẫn trả về.
	run := stubRun(t, []string{"fail", "fail"}, []string{cleanContent + " Bản đã sửa."})
	batch, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), loopTestItems(), loopTestBatch(cleanContent))
	if meta["verdict"] != "revise_exhausted" {
		t.Fatalf("expected revise_exhausted, got %v", meta["verdict"])
	}
	if batch == nil || len(batch["posts"].([]map[string]any)) != 1 {
		t.Fatal("exhausted review must still return a batch")
	}
}

func TestRunDraftReviewSkipsMultiPostBatch(t *testing.T) {
	items := append(loopTestItems(), SourceItem{SourceIndex: 2, ChecklistItem: "row2"})
	called := false
	run := func(agent.RunRequest) error { called = true; return nil }
	_, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), items, loopTestBatch(cleanContent))
	if called || meta["verdict"] != "skipped_multi_post" {
		t.Fatalf("multi-post batch must skip the loop entirely, got %+v", meta)
	}
}

func TestRunDraftReviewReviewErrorNeverFailsTheJob(t *testing.T) {
	run := func(req agent.RunRequest) error { return context.DeadlineExceeded }
	batch, meta := runDraftReview(run, agent.RunRequest{}, map[string]any{}, loopTestConfig(), loopTestItems(), loopTestBatch(cleanContent))
	if meta["verdict"] != "review_error" || batch == nil {
		t.Fatalf("review failure must keep the batch and flag review_error, got %+v", meta)
	}
}
