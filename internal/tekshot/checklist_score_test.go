package tekshot

import (
	"strings"
	"testing"
)

func TestNormalizeChecklistScoreComputesTheTotalInCode(t *testing.T) {
	scores := validScoreMap()
	scores["tong"] = float64(15)

	normalised, err := normalizeChecklistScore(scores, "items[0]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalised["tong"] != 10 {
		t.Fatalf("five criteria of 2 make 10, whatever the model claims; got %v", normalised["tong"])
	}
}

func TestNormalizeChecklistScoreRefusesBadScores(t *testing.T) {
	cases := map[string]func(scores map[string]any){
		"out of range": func(s map[string]any) {
			s["dung_luc"] = map[string]any{"diem": float64(4), "ly_do": "Đúng dịp cuối năm."}
		},
		"fraction": func(s map[string]any) {
			s["dung_luc"] = map[string]any{"diem": 1.5, "ly_do": "Đúng dịp cuối năm."}
		},
		"missing reason": func(s map[string]any) { s["co_nguon"] = map[string]any{"diem": float64(2), "ly_do": "  "} },
		"missing key":    func(s map[string]any) { delete(s, "hop_muc_tieu") },
		"not a number": func(s map[string]any) {
			s["dung_thu_ban"] = map[string]any{"diem": "cao", "ly_do": "Khớp món chính."}
		},
	}
	for name, mutate := range cases {
		scores := validScoreMap()
		mutate(scores)
		if _, err := normalizeChecklistScore(scores, "items[1]"); err == nil || !strings.Contains(err.Error(), "items[1]") {
			t.Fatalf("%s: expected a refusal naming the row, got %v", name, err)
		}
	}
	if _, err := normalizeChecklistScore(nil, "items[0]"); err == nil {
		t.Fatal("a row without any score must be refused")
	}
}

func TestChecklistScorePropertyIsStrictOnlyForTheGeneratingJob(t *testing.T) {
	strict := checklistScoreProperty(true)
	if strict["additionalProperties"] != false || len(strict["required"].([]string)) != len(checklistScoreCriteria) {
		t.Fatalf("the planning job must require all five criteria: %v", strict)
	}
	loose := checklistScoreProperty(false)
	if _, hasRequired := loose["required"]; hasRequired {
		t.Fatal("the chat schema must allow keep/delete rows with an empty score")
	}
}

func TestChecklistFixtureRowPassesTheWholeValidator(t *testing.T) {
	item := validChecklistItem()
	if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err != nil {
		t.Fatalf("fixture must be valid: %v", err)
	}
	score := item["diem_chu_de"].(map[string]any)
	if score["tong"] != 10 {
		t.Fatalf("validator must store the computed total, got %v", score["tong"])
	}
}

func TestChecklistPlanFieldsRefuseUnknownHookAndStory(t *testing.T) {
	for _, key := range []string{"kieu_hook", "cot_truyen"} {
		item := validChecklistItem()
		item[key] = "KHAC"
		if err := validateChecklistPlanFields(item, testPlanFrame(), 0); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s: expected a refusal, got %v", key, err)
		}
	}
}
