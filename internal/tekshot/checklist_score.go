package tekshot

import (
	"fmt"
	"strings"
)

// Điểm chủ đề /15: năm tiêu chí, mỗi tiêu chí 0-3 kèm lý do. Tiêu chí thứ tư
// thay "làm được tại Việt Nam" của tài liệu gốc (viết cho thẻ xu hướng quốc tế)
// bằng "hợp mục tiêu page", đọc từ hồ sơ doanh nghiệp.
const (
	checklistScoreMax       = 3
	checklistScoreThreshold = 10
	checklistScoreMinReason = 5
)

type checklistScoreCriterion struct {
	Key     string
	Label   string
	Meaning string
}

var checklistScoreCriteria = []checklistScoreCriterion{
	{"dung_luc", "Đúng lúc", "fits a season, occasion or trend of the last 90 days (0 = no timing at all)"},
	{"co_nguon", "Có nguồn", "rests on real facts given above: research, Vault, profile, page data (0 = invented)"},
	{"dung_thu_ban", "Đúng thứ đang bán", "matches what the business profile says it sells (0 = unrelated)"},
	{"hop_muc_tieu", "Hợp mục tiêu trang", "serves the page goal in the profile: selling, leads, recruiting... (0 = off goal)"},
	{"khach_quan_tam", "Khách đang quan tâm", "there is a signal the audience cares: comments, messages, top posts, research (0 = no signal)"},
}

// checklistScoreProperty là schema điểm. strict=true cho job lập kế hoạch;
// chat để lỏng vì dòng keep/delete không có điểm.
func checklistScoreProperty(strict bool) map[string]any {
	description := "Score the topic on five criteria, each 0-3 with a one-sentence reason in Vietnamese."
	if !strict {
		return map[string]any{"type": "object", "description": description + " Required for create/update rows; keep/delete rows may send {}."}
	}
	properties := map[string]any{}
	required := make([]string, 0, len(checklistScoreCriteria))
	for _, criterion := range checklistScoreCriteria {
		properties[criterion.Key] = map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"description":          criterion.Label + ": " + criterion.Meaning,
			"properties": map[string]any{
				"diem":  map[string]any{"type": "integer", "minimum": 0, "maximum": checklistScoreMax},
				"ly_do": map[string]any{"type": "string", "description": "One sentence, Vietnamese, citing the fact behind the score."},
			},
			"required": []string{"diem", "ly_do"},
		}
		required = append(required, criterion.Key)
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"description":          description,
		"properties":           properties,
		"required":             required,
	}
}

// normalizeChecklistScore kiểm và tính tổng bằng code — không tin tổng model tự
// cộng. Lỗi trả về để model chấm lại.
func normalizeChecklistScore(raw any, label string) (map[string]any, error) {
	scores, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.diem_chu_de must score all five criteria", label)
	}
	out := map[string]any{}
	total := 0
	for _, criterion := range checklistScoreCriteria {
		entry, ok := scores[criterion.Key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.diem_chu_de.%s is required", label, criterion.Key)
		}
		var value float64
		switch v := entry["diem"].(type) {
		case float64:
			value = v
		case int:
			value = float64(v)
		default:
			return nil, fmt.Errorf("%s.diem_chu_de.%s.diem must be a number 0-%d", label, criterion.Key, checklistScoreMax)
		}
		if value != float64(int(value)) || value < 0 || value > checklistScoreMax {
			return nil, fmt.Errorf("%s.diem_chu_de.%s.diem must be a whole number 0-%d, got %v", label, criterion.Key, checklistScoreMax, value)
		}
		reason := strings.TrimSpace(stringFromMap(entry, "ly_do"))
		if len([]rune(reason)) < checklistScoreMinReason {
			return nil, fmt.Errorf("%s.diem_chu_de.%s.ly_do is required: name the fact behind the score", label, criterion.Key)
		}
		out[criterion.Key] = map[string]any{"diem": int(value), "ly_do": reason}
		total += int(value)
	}
	out["tong"] = total
	return out, nil
}

// writeChecklistScoreRules là thang chấm đưa cho người lập kế hoạch và người chấm.
func writeChecklistScoreRules(sb *strings.Builder) {
	sb.WriteString(fmt.Sprintf("## Topic score (diem_chu_de) — five criteria, each 0-%d, total out of %d\n", checklistScoreMax, checklistScoreMax*len(checklistScoreCriteria)))
	for _, criterion := range checklistScoreCriteria {
		sb.WriteString("- " + criterion.Key + " (" + criterion.Label + "): " + criterion.Meaning + "\n")
	}
	sb.WriteString(fmt.Sprintf("- A topic totalling below %d is still shown but marked \"low score\" for the team to decide. Score honestly: an inflated score hides a weak topic, and every ly_do must name the fact behind its score.\n\n", checklistScoreThreshold))
}
