package tekshot

import (
	"context"
	"strings"
	"testing"
)

func reviewTestCriteria() []ReviewCriterion {
	return []ReviewCriterion{
		{ID: "brief_coverage", Description: "Mọi ý trong brief được triển khai.", Critical: true},
		{ID: "cta_quality", Description: "CTA rõ ràng.", Critical: false},
	}
}

const reviewTestContent = "Tháng này hóa đơn điện tăng gấp đôi? Điều hòa inverter mới cắt 40% điện năng. Inbox ngay để được tư vấn công suất phù hợp."

func validReviewArgs() map[string]any {
	return map[string]any{
		"verdict": "pass",
		"criteria": []any{
			map[string]any{"id": "brief_coverage", "verdict": "pass", "evidence": "", "note": ""},
			map[string]any{"id": "cta_quality", "verdict": "pass", "evidence": "", "note": ""},
		},
		"revision_notes": "",
	}
}

func TestPostReviewCollectorAcceptsValidPass(t *testing.T) {
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	res := tool.Execute(context.Background(), validReviewArgs())
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	got := tool.Result()
	if got == nil || !got.Passed {
		t.Fatalf("expected passed result, got %+v", got)
	}
}

func TestPostReviewCollectorDerivesFailFromCriticalCriterion(t *testing.T) {
	args := validReviewArgs()
	// Model khai verdict tổng "pass" nhưng criterion critical fail → Go phải suy ra fail.
	args["criteria"].([]any)[0] = map[string]any{
		"id": "brief_coverage", "verdict": "fail",
		"evidence": "Inbox ngay để được tư vấn công suất phù hợp.",
		"note":     "Brief có ý về bảo hành nhưng caption không nhắc.",
	}
	args["revision_notes"] = "Bổ sung ý bảo hành từ brief."
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	if res := tool.Execute(context.Background(), args); res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	got := tool.Result()
	if got == nil || got.Passed {
		t.Fatal("critical fail must derive Passed=false regardless of overall verdict")
	}
	if len(got.FailedCritical) != 1 || got.FailedCritical[0] != "brief_coverage" {
		t.Fatalf("expected FailedCritical=[brief_coverage], got %v", got.FailedCritical)
	}
}

func TestPostReviewCollectorRejectsFailWithoutEvidence(t *testing.T) {
	args := validReviewArgs()
	args["criteria"].([]any)[0] = map[string]any{"id": "brief_coverage", "verdict": "fail", "evidence": "", "note": "x"}
	args["revision_notes"] = "x"
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	res := tool.Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "MODEL_OUTPUT_INVALID") {
		t.Fatal("fail without evidence must be rejected")
	}
}

func TestPostReviewCollectorRejectsFabricatedEvidence(t *testing.T) {
	args := validReviewArgs()
	args["criteria"].([]any)[0] = map[string]any{
		"id": "brief_coverage", "verdict": "fail",
		"evidence": "Câu này không hề tồn tại trong content.", "note": "x",
	}
	args["revision_notes"] = "x"
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	if res := tool.Execute(context.Background(), args); !res.IsError {
		t.Fatal("evidence not found in content must be rejected")
	}
}

func TestPostReviewCollectorRejectsMissingOrUnknownCriterion(t *testing.T) {
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	args := validReviewArgs()
	args["criteria"] = []any{args["criteria"].([]any)[0]} // thiếu cta_quality
	if res := tool.Execute(context.Background(), args); !res.IsError {
		t.Fatal("missing criterion id must be rejected")
	}
	args2 := validReviewArgs()
	args2["criteria"] = append(args2["criteria"].([]any), map[string]any{"id": "made_up", "verdict": "pass", "evidence": "", "note": ""})
	if res := tool.Execute(context.Background(), args2); !res.IsError {
		t.Fatal("unknown criterion id must be rejected")
	}
}

func TestPostReviewCollectorRequiresRevisionNotesOnAnyFail(t *testing.T) {
	args := validReviewArgs()
	args["criteria"].([]any)[1] = map[string]any{
		"id": "cta_quality", "verdict": "fail",
		"evidence": "Inbox ngay để được tư vấn công suất phù hợp.", "note": "CTA chung chung.",
	}
	args["revision_notes"] = ""
	tool := NewPostReviewCollectorTool(reviewTestCriteria(), reviewTestContent)
	if res := tool.Execute(context.Background(), args); !res.IsError {
		t.Fatal("any fail requires non-empty revision_notes")
	}
}
