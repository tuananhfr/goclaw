package tekshot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Các page khác cùng chủ (cùng cửa hàng, kể cả page đã bỏ gắn cửa hàng) giành
// cùng người xem: không đăng sát giờ nhau, không dùng chung từ khoá CTA, không
// cùng chủ đề cho cùng tệp khách trong ít ngày.
const (
	checklistSiblingMinGapMinutes = 120
	checklistSiblingKeywordDays   = 30
	checklistSiblingTopicDays     = 7
	checklistSiblingPromptRows    = 40
)

type checklistSiblingRow struct {
	Date     string
	TimeSlot string
	Topic    string
	Audience string
	Keyword  string
}

type checklistSiblingPage struct {
	Name        string
	OwnedTopics []string
	Rows        []checklistSiblingRow
}

func checklistSiblingsFromRequest(request map[string]any) []checklistSiblingPage {
	raw, _ := request["siblings"].([]any)
	pages := make([]checklistSiblingPage, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		page := checklistSiblingPage{
			Name:        strings.TrimSpace(stringFromMap(item, "page")),
			OwnedTopics: stringSliceFromAny(item["owned_topics"]),
		}
		rows, _ := item["rows"].([]any)
		for _, rawRow := range rows {
			row, ok := rawRow.(map[string]any)
			if !ok || strings.TrimSpace(stringFromMap(row, "topic")) == "" {
				continue
			}
			page.Rows = append(page.Rows, checklistSiblingRow{
				Date:     strings.TrimSpace(stringFromMap(row, "date")),
				TimeSlot: strings.TrimSpace(stringFromMap(row, "time_slot")),
				Topic:    strings.TrimSpace(stringFromMap(row, "topic")),
				Audience: strings.TrimSpace(stringFromMap(row, "audience")),
				Keyword:  strings.ToUpper(strings.TrimSpace(stringFromMap(row, "keyword"))),
			})
		}
		if page.Name != "" && (len(page.Rows) > 0 || len(page.OwnedTopics) > 0) {
			pages = append(pages, page)
		}
	}
	return pages
}

func writeChecklistSiblings(sb *strings.Builder, pages []checklistSiblingPage) {
	if len(pages) == 0 {
		return
	}
	sb.WriteString("## Other pages of the same owner (do not compete with them)\n")
	written := 0
	for _, page := range pages {
		sb.WriteString("- Page \"" + page.Name + "\"")
		if len(page.OwnedTopics) > 0 {
			sb.WriteString(" owns the topics: " + strings.Join(page.OwnedTopics, "; ") + " — do not plan those for this page")
		}
		sb.WriteString("\n")
		for _, row := range page.Rows {
			if written >= checklistSiblingPromptRows {
				break
			}
			line := "  - " + row.Date
			if row.TimeSlot != "" {
				line += " " + row.TimeSlot
			}
			line += " " + row.Topic
			var tags []string
			if row.Audience != "" {
				tags = append(tags, "audience "+row.Audience)
			}
			if row.Keyword != "" {
				tags = append(tags, "keyword "+row.Keyword)
			}
			if len(tags) > 0 {
				line += " (" + strings.Join(tags, ", ") + ")"
			}
			sb.WriteString(line + "\n")
			written++
		}
	}
	sb.WriteString(fmt.Sprintf("- On a day another page posts, keep this page's time_slot at least %d hours apart from it.\n", checklistSiblingMinGapMinutes/60))
	sb.WriteString("- Never reuse another page's tu_khoa_cta: the reply bot could not tell which page the reader means.\n")
	sb.WriteString(fmt.Sprintf("- Do not plan the same topic as another page within %d days for the same customer group; a different group or a clearly different angle is fine.\n\n", checklistSiblingTopicDays))
}

// Giờ bắt đầu của khung giờ: "19:00-20:00" là 19:00, "9h30" là 09:30.
var checklistSlotPattern = regexp.MustCompile(`(\d{1,2})\s*[:hH]\s*(\d{2})?`)

func checklistSlotMinutes(slot string) (int, bool) {
	match := checklistSlotPattern.FindStringSubmatch(slot)
	if match == nil {
		return 0, false
	}
	hours, err := strconv.Atoi(match[1])
	if err != nil || hours > 23 {
		return 0, false
	}
	minutes := 0
	if match[2] != "" {
		minutes, _ = strconv.Atoi(match[2])
		if minutes > 59 {
			return 0, false
		}
	}
	return hours*60 + minutes, true
}

func checklistDaysApart(a, b string) (int, bool) {
	left, okLeft := parseChecklistDay(a)
	right, okRight := parseChecklistDay(b)
	if !okLeft || !okRight {
		return 0, false
	}
	days := int(left.Sub(right).Hours() / 24)
	if days < 0 {
		days = -days
	}
	return days, true
}

func formatSlot(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// checklistSiblingFindings kiểm ba luật giữa các page cùng chủ, theo thứ tự plan.
func checklistSiblingFindings(plan []checklistPlanEntry, pages []checklistSiblingPage) [][]string {
	findings := make([][]string, len(plan))
	for i, entry := range plan {
		add := func(message string) {
			if len(findings[i]) < checklistFindingsPerRow {
				findings[i] = append(findings[i], message)
			}
		}
		ownMinutes, ownTimed := checklistSlotMinutes(entry.TimeSlot)

		for _, page := range pages {
			for _, row := range page.Rows {
				if ownTimed && entry.Date != "" && entry.Date == row.Date {
					if theirs, ok := checklistSlotMinutes(row.TimeSlot); ok {
						gap := ownMinutes - theirs
						if gap < 0 {
							gap = -gap
						}
						if gap < checklistSiblingMinGapMinutes {
							add(fmt.Sprintf("đăng %s cùng ngày với «%s» của page %s lúc %s (cần lệch từ %d giờ)",
								formatSlot(ownMinutes), row.Topic, page.Name, formatSlot(theirs), checklistSiblingMinGapMinutes/60))
						}
					}
				}

				if entry.Keyword != "" && strings.EqualFold(entry.Keyword, row.Keyword) {
					days, dated := checklistDaysApart(entry.Date, row.Date)
					if !dated || days <= checklistSiblingKeywordDays {
						add(fmt.Sprintf("từ khoá CTA «%s» trùng với bài «%s» của page %s — bot không biết khách hỏi page nào",
							entry.Keyword, row.Topic, page.Name))
					}
				}

				sameAudience := entry.Audience == "" || row.Audience == "" || strings.EqualFold(entry.Audience, row.Audience)
				if !sameAudience {
					continue
				}
				days, dated := checklistDaysApart(entry.Date, row.Date)
				if !dated || days > checklistSiblingTopicDays {
					continue
				}
				if ratio, ok := topicOverlap(entry.Topic, row.Topic); ok && ratio >= checklistTopicOverlapMax {
					add(fmt.Sprintf("chủ đề trùng khoảng %d%% với «%s» của page %s, cùng tệp khách, cách %d ngày",
						int(ratio*100), row.Topic, page.Name, days))
				}
			}
		}
	}
	return findings
}

// mergeChecklistFindings gộp cảnh báo lặp trong page và giữa các page, theo từng dòng.
func mergeChecklistFindings(groups ...[][]string) [][]string {
	if len(groups) == 0 {
		return nil
	}
	merged := make([][]string, len(groups[0]))
	for _, group := range groups {
		for i := range merged {
			if i < len(group) {
				merged[i] = append(merged[i], group[i]...)
			}
		}
	}
	return merged
}
