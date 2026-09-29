package tekshot

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Kiểu hook và cốt truyện của một dòng checklist. Cùng bộ với
// InsightChecklistPlanFields.php và checklistPlanFields.ts: đổi một mã là ba
// nơi phải đổi cùng lúc.
var (
	checklistHookTypes  = []string{"CAU_HOI", "CON_SO", "CAU_CHUYEN", "NGHICH_LY", "MEO_NHANH", "TUYEN_BO", "SO_SANH"}
	checklistStoryTypes = []string{"TRUOC_SAU", "HAU_TRUONG", "CHUYEN_KHACH", "HUONG_DAN", "SO_SANH", "SAI_LAM", "DIP_SU_KIEN", "GIOI_THIEU"}
)

const (
	// Cốt truyện không lặp trong ngần ấy ngày; kiểu hook không lặp hai bài liền nhau.
	checklistStoryNoRepeatDays = 14
	// Chủ đề trùng từ ngần ấy cặp từ trở lên so với bài gần đây thì coi là lặp.
	checklistTopicOverlapMax = 0.4
	// Tiêu đề dưới 4 từ có quá ít cặp từ để so: hai câu ngắn trùng một cặp là ngẫu nhiên.
	checklistTopicMinBigrams = 3
	// Số lần được viết lại vì lặp; sau đó nhận dòng kèm cảnh báo cho người quyết.
	checklistMaxRepeatRewrites = 2
	// Trần số mục lịch sử đưa vào prompt.
	checklistHistoryPromptLimit = 60
	checklistFindingsPerRow     = 3
)

// checklistHistoryEntry là một chủ đề đã có: dòng checklist trước đó hoặc bài
// đã đăng lên page. Bài đăng không có kiểu hook / cốt truyện.
type checklistHistoryEntry struct {
	ID        int
	Date      string
	Topic     string
	HookType  string
	StoryType string
	Source    string
}

func checklistHistoryFromRequest(request map[string]any) []checklistHistoryEntry {
	raw, _ := request["history"].([]any)
	out := make([]checklistHistoryEntry, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		topic := strings.TrimSpace(stringFromMap(item, "topic"))
		if topic == "" {
			continue
		}
		out = append(out, checklistHistoryEntry{
			ID:        int(numberFromMap(item, "id")),
			Date:      strings.TrimSpace(stringFromMap(item, "date")),
			Topic:     topic,
			HookType:  strings.ToUpper(strings.TrimSpace(stringFromMap(item, "hook_type"))),
			StoryType: strings.ToUpper(strings.TrimSpace(stringFromMap(item, "story_type"))),
			Source:    strings.TrimSpace(stringFromMap(item, "source")),
		})
	}
	return out
}

func writeChecklistHistory(sb *strings.Builder, history []checklistHistoryEntry) {
	sb.WriteString("## Recent topics of this page (do not repeat)\n")
	if len(history) == 0 {
		sb.WriteString("- (none)\n\n")
		return
	}
	for i, entry := range history {
		if i >= checklistHistoryPromptLimit {
			break
		}
		line := "- " + entry.Date
		if entry.Source != "" {
			line += " [" + entry.Source + "]"
		}
		line += " " + entry.Topic
		var tags []string
		if entry.HookType != "" {
			tags = append(tags, "hook "+entry.HookType)
		}
		if entry.StoryType != "" {
			tags = append(tags, "story "+entry.StoryType)
		}
		if len(tags) > 0 {
			line += " (" + strings.Join(tags, ", ") + ")"
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n")
}

// checklistPlanEntry là một dòng đang được kiểm.
type checklistPlanEntry struct {
	Label     string
	ID        int
	Date      string
	Topic     string
	HookType  string
	StoryType string
}

// topicBigrams gộp Unicode về một dạng: cùng chữ "tối" mà một bên viết dựng sẵn,
// một bên tách dấu thì vẫn phải khớp.
func topicBigrams(topic string) map[string]struct{} {
	tokens := strings.FieldsFunc(strings.ToLower(norm.NFC.String(topic)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	set := map[string]struct{}{}
	for i := 0; i+1 < len(tokens); i++ {
		set[tokens[i]+" "+tokens[i+1]] = struct{}{}
	}
	return set
}

// topicOverlap trả tỉ lệ cặp từ trùng trên tiêu đề ngắn hơn; ok=false khi một
// bên quá ngắn để so.
func topicOverlap(a, b string) (float64, bool) {
	left, right := topicBigrams(a), topicBigrams(b)
	if len(left) < checklistTopicMinBigrams || len(right) < checklistTopicMinBigrams {
		return 0, false
	}
	shared := 0
	for pair := range left {
		if _, found := right[pair]; found {
			shared++
		}
	}
	shorter := len(left)
	if len(right) < shorter {
		shorter = len(right)
	}
	return float64(shared) / float64(shorter), true
}

func parseChecklistDay(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return parsed, err == nil
}

// planEarlier xếp theo (ngày, vị trí) để một cặp trùng chỉ gắn cờ dòng đứng sau.
func planEarlier(a, b checklistPlanEntry, ai, bi int) bool {
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return ai < bi
}

// checklistRepetitionFindings kiểm ba luật lặp trong code, không nhờ model:
// kiểu hook liền nhau, cốt truyện trong 14 ngày, chủ đề trùng cặp từ. Kết quả
// theo đúng thứ tự plan; dòng không vướng thì rỗng.
func checklistRepetitionFindings(plan []checklistPlanEntry, history []checklistHistoryEntry) [][]string {
	findings := make([][]string, len(plan))

	planIDs := map[int]bool{}
	for _, entry := range plan {
		if entry.ID > 0 {
			planIDs[entry.ID] = true
		}
	}
	others := make([]checklistHistoryEntry, 0, len(history))
	for _, entry := range history {
		if entry.ID > 0 && planIDs[entry.ID] {
			continue
		}
		others = append(others, entry)
	}

	add := func(index int, message string) {
		if len(findings[index]) < checklistFindingsPerRow {
			findings[index] = append(findings[index], message)
		}
	}

	// Kiểu hook: bài liền trước trên trục thời gian (lịch sử + kế hoạch).
	type node struct {
		date, hook, topic string
		plan              int
	}
	timeline := make([]node, 0, len(others)+len(plan))
	for _, entry := range others {
		timeline = append(timeline, node{date: entry.Date, hook: entry.HookType, topic: entry.Topic, plan: -1})
	}
	for i, entry := range plan {
		timeline = append(timeline, node{date: entry.Date, hook: entry.HookType, topic: entry.Topic, plan: i})
	}
	sort.SliceStable(timeline, func(i, j int) bool { return timeline[i].date < timeline[j].date })
	for i := 1; i < len(timeline); i++ {
		current, previous := timeline[i], timeline[i-1]
		if current.plan >= 0 && current.hook != "" && current.hook == previous.hook {
			add(current.plan, fmt.Sprintf("kiểu hook %s lặp bài liền trước «%s»", current.hook, previous.topic))
		}
	}

	for i, entry := range plan {
		day, dated := parseChecklistDay(entry.Date)

		// Cốt truyện: cùng loại trong vòng 14 ngày.
		if entry.StoryType != "" && dated {
			within := func(otherDate, otherTopic string) {
				other, ok := parseChecklistDay(otherDate)
				if !ok {
					return
				}
				gap := int(day.Sub(other).Hours() / 24)
				if gap < 0 {
					gap = -gap
				}
				if gap < checklistStoryNoRepeatDays {
					add(i, fmt.Sprintf("cốt truyện %s cách bài «%s» chỉ %d ngày (cần từ %d ngày)", entry.StoryType, otherTopic, gap, checklistStoryNoRepeatDays))
				}
			}
			for _, other := range others {
				if other.StoryType == entry.StoryType {
					within(other.Date, other.Topic)
				}
			}
			for j, other := range plan {
				if j != i && other.StoryType == entry.StoryType && planEarlier(other, entry, j, i) {
					within(other.Date, other.Topic)
				}
			}
		}

		// Chủ đề trùng cặp từ.
		for _, other := range others {
			if ratio, ok := topicOverlap(entry.Topic, other.Topic); ok && ratio >= checklistTopicOverlapMax {
				add(i, fmt.Sprintf("chủ đề trùng khoảng %d%% với «%s»", int(ratio*100), other.Topic))
			}
		}
		for j, other := range plan {
			if j == i || !planEarlier(other, entry, j, i) {
				continue
			}
			if ratio, ok := topicOverlap(entry.Topic, other.Topic); ok && ratio >= checklistTopicOverlapMax {
				add(i, fmt.Sprintf("chủ đề trùng khoảng %d%% với «%s»", int(ratio*100), other.Topic))
			}
		}
	}
	return findings
}

// enforceChecklistRepetition bắt model viết lại các dòng lặp — tối đa
// checklistMaxRepeatRewrites lần; sau đó nhận dòng và gắn canh_bao_lap để người
// quyết. Dòng keep/delete của chat không được kiểm: chúng không đổi nội dung.
func enforceChecklistRepetition(items []any, history []checklistHistoryEntry, rejects *int) error {
	var plan []checklistPlanEntry
	var itemAt []map[string]any
	dropped := map[int]bool{}
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		action := strings.TrimSpace(stringFromMap(item, "action"))
		id := int(numberFromMap(item, "source_item_id"))
		if action == "delete" && id > 0 {
			dropped[id] = true
		}
		if action == "keep" || action == "delete" {
			continue
		}
		plan = append(plan, checklistPlanEntry{
			Label:     fmt.Sprintf("items[%d]", i),
			ID:        id,
			Date:      strings.TrimSpace(stringFromMap(item, "date")),
			Topic:     strings.TrimSpace(stringFromMap(item, "topic")),
			HookType:  strings.ToUpper(strings.TrimSpace(stringFromMap(item, "kieu_hook"))),
			StoryType: strings.ToUpper(strings.TrimSpace(stringFromMap(item, "cot_truyen"))),
		})
		itemAt = append(itemAt, item)
	}
	if len(dropped) > 0 {
		kept := make([]checklistHistoryEntry, 0, len(history))
		for _, entry := range history {
			if entry.ID > 0 && dropped[entry.ID] {
				continue
			}
			kept = append(kept, entry)
		}
		history = kept
	}

	findings := checklistRepetitionFindings(plan, history)
	var lines []string
	for i, rowFindings := range findings {
		if len(rowFindings) > 0 {
			lines = append(lines, plan[i].Label+": "+strings.Join(rowFindings, "; "))
		}
	}

	if len(lines) > 0 && *rejects < checklistMaxRepeatRewrites {
		*rejects++
		return fmt.Errorf("REPETITION: %s. Rewrite ONLY those rows with a different hook type, story type or topic angle, then resubmit the whole plan (rewrite %d of %d)",
			strings.Join(lines, " | "), *rejects, checklistMaxRepeatRewrites)
	}

	for i, rowFindings := range findings {
		if len(rowFindings) == 0 {
			delete(itemAt[i], "canh_bao_lap")
			continue
		}
		warnings := make([]any, len(rowFindings))
		for j, message := range rowFindings {
			warnings[j] = message
		}
		itemAt[i]["canh_bao_lap"] = warnings
	}
	return nil
}
