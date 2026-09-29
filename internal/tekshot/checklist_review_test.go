package tekshot

import (
	"encoding/json"
	"strings"
	"testing"
)

func reviewReply(t *testing.T, rows ...map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		t.Fatal(err)
	}
	return "Đây là kết quả:\n" + string(encoded)
}

func reviewRow(id int, topic string) map[string]any {
	return map[string]any{"id": float64(id), "date": "2026-10-01", "topic": topic}
}

func TestChecklistReviewKeepsTheUsersHookTypeAndFillsTheEmptyStoryType(t *testing.T) {
	rows := []map[string]any{reviewRow(4, "Chủ đề tự gõ")}
	rows[0]["kieu_hook"] = "TUYEN_BO"

	reply := reviewReply(t, map[string]any{
		"id": float64(4), "kieu_hook": "CAU_HOI", "cot_truyen": "huong_dan",
		"nhan_xet": "Chủ đề rõ ràng.", "diem_chu_de": validScoreMap(),
	})
	result, err := buildChecklistReviewResult(reply, rows, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	row := result["rows"].([]any)[0].(map[string]any)
	if row["kieu_hook"] != "TUYEN_BO" {
		t.Fatalf("the user's own hook type must win, got %v", row["kieu_hook"])
	}
	if row["cot_truyen"] != "HUONG_DAN" {
		t.Fatalf("an empty story type takes the reviewer's, upper-cased; got %v", row["cot_truyen"])
	}
	if row["diem_chu_de"].(map[string]any)["tong"] != 10 {
		t.Fatalf("expected the computed total, got %v", row["diem_chu_de"])
	}
}

func TestChecklistReviewFlagsRepetitionInCodeAgainstHistory(t *testing.T) {
	history := []checklistHistoryEntry{{ID: 1, Date: "2026-09-25", Topic: "Khung 7 số quản lý chuỗi nhỏ nên xem mỗi ngày", Source: "posted"}}
	rows := []map[string]any{reviewRow(4, "Khung 7 số quản lý chuỗi nhỏ nên xem mỗi tuần")}

	reply := reviewReply(t, map[string]any{"id": float64(4), "diem_chu_de": validScoreMap()})
	result, err := buildChecklistReviewResult(reply, rows, history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	warnings := result["rows"].([]any)[0].(map[string]any)["canh_bao_lap"].([]any)
	if len(warnings) != 1 || !strings.Contains(warnings[0].(string), "chủ đề trùng") {
		t.Fatalf("expected the code to flag the near-duplicate topic, got %v", warnings)
	}
}

func TestChecklistReviewDropsAnInvalidScoreButKeepsTheRest(t *testing.T) {
	rows := []map[string]any{reviewRow(4, "Chủ đề một"), reviewRow(5, "Chủ đề hai")}
	broken := validScoreMap()
	broken["dung_luc"] = map[string]any{"diem": float64(9), "ly_do": "Sai thang điểm."}

	reply := reviewReply(t,
		map[string]any{"id": float64(4), "diem_chu_de": broken},
		map[string]any{"id": float64(5), "diem_chu_de": validScoreMap()},
	)
	result, err := buildChecklistReviewResult(reply, rows, nil)
	if err != nil {
		t.Fatalf("one good row is enough: %v", err)
	}
	out := result["rows"].([]any)
	first, second := out[0].(map[string]any), out[1].(map[string]any)
	if _, scored := first["diem_chu_de"]; scored || first["loi"] == nil {
		t.Fatalf("the broken row must carry an error and no score, got %v", first)
	}
	if _, scored := second["diem_chu_de"]; !scored {
		t.Fatalf("the valid row must keep its score, got %v", second)
	}
}

func TestChecklistReviewFailsClosedWhenNothingCanBeScored(t *testing.T) {
	rows := []map[string]any{reviewRow(4, "Chủ đề một")}

	if _, err := buildChecklistReviewResult("không phải JSON", rows, nil); err == nil {
		t.Fatal("an unreadable reply must fail the job")
	}
	if _, err := buildChecklistReviewResult(reviewReply(t, map[string]any{"id": float64(99), "diem_chu_de": validScoreMap()}), rows, nil); err == nil {
		t.Fatal("a reply that scores no requested row must fail the job")
	}
}

func TestChecklistReviewRowsSkipEntriesThatCannotBeReviewed(t *testing.T) {
	rows := checklistReviewRows(map[string]any{"rows": []any{
		map[string]any{"id": float64(1), "topic": "Có chủ đề"},
		map[string]any{"id": float64(2), "topic": " "},
		map[string]any{"topic": "Không có id"},
	}})
	if len(rows) != 1 {
		t.Fatalf("expected one reviewable row, got %d", len(rows))
	}
}

func TestChecklistReviewPromptCarriesTheRubricAndTheRows(t *testing.T) {
	request := map[string]any{"page_name": "Tekshot", "history": []any{
		map[string]any{"date": "2026-09-01", "topic": "Bài cũ", "source": "posted"},
	}}
	prompt := buildChecklistReviewPrompt(request, []map[string]any{reviewRow(4, "Chủ đề tự gõ")})
	for _, expected := range []string{"dung_luc", "khach_quan_tam", "Recent topics of this page", "Bài cũ", "- id: 4", "Chủ đề tự gõ", "JSON only"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}
}

func TestChecklistReviewIsARegisteredJobType(t *testing.T) {
	if !isSupportedTekshotJobType(TekshotJobTypeChecklistReview) {
		t.Fatal("the review job type must be accepted by the job queue")
	}
}
