package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

const (
	// contentFixRounds: số lượt sửa bài tối đa sau bản đầu (chủ page chốt 2).
	contentFixRounds = 2
	// Số không có nguồn là bịa: kéo bài xuống dưới mốc để vòng sửa chắc chắn chạy.
	contentCodeDefectMaxScore = 6
)

// contentNumberPattern bắt số liệu kiểu 30%, 1.200, 45,5 %. Số một chữ số trần
// ("3 bước", "bước 1") bị bỏ qua: đó là đánh số, không phải dữ kiện.
var contentNumberPattern = regexp.MustCompile(`\d+(?:[.,]\d+)*\s*%?`)

func buildContentReviewPrompt(post map[string]any, source string, facts string) string {
	var sb strings.Builder
	sb.WriteString("Bạn là biên tập viên ĐỘC LẬP soát một bài Facebook. Bạn không viết bài này và không biết người viết nghĩ gì — chỉ đánh giá chữ đang có, đối chiếu với nguồn.\n\n")

	sb.WriteString("## Bài cần soát\n")
	sb.WriteString("- Tiêu đề: " + strings.TrimSpace(stringArg(post, "title")) + "\n")
	sb.WriteString("- Brief: " + strings.TrimSpace(stringArg(post, "brief")) + "\n")
	sb.WriteString("- Nội dung:\n" + strings.TrimSpace(stringArg(post, "content")) + "\n\n")

	sb.WriteString("## Nguồn sự thật (chỉ những gì ở đây mới được coi là có thật)\n")
	if strings.TrimSpace(source) != "" {
		sb.WriteString("Dòng checklist:\n" + strings.TrimSpace(source) + "\n")
	}
	if strings.TrimSpace(facts) != "" {
		sb.WriteString("Dữ kiện đã tra:\n" + strings.TrimSpace(facts) + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Soát theo đúng 7 mục, chỉ báo lỗi THẬT, không báo gu\n")
	sb.WriteString("- SU_THAT: con số, giá, tên, lời hứa hay kết quả không có trong nguồn.\n")
	sb.WriteString("- THIEU_Y: tiêu đề hứa điều gì mà bài không làm, hoặc bỏ sót dữ kiện quan trọng có sẵn trong nguồn.\n")
	sb.WriteString("- CHUNG_CHUNG: câu đặt vào bài của page khác vẫn đúng, câu sáo, tính từ độn.\n")
	sb.WriteString("- MO_BAI: 3 dòng đầu không bắt đúng vấn đề hoặc kết quả mà tiêu đề hứa.\n")
	sb.WriteString("- CTA: thiếu lời kêu gọi, hoặc kêu gọi không khớp bài.\n")
	sb.WriteString("- META: bài nhắc tới nguồn, checklist, việc tra cứu, hay bàn chuyện số liệu lệch nhau.\n")
	sb.WriteString("- NGON_NGU: sai chính tả, câu lủng củng, đơn vị hoặc từ nước ngoài chưa đổi cho người Việt.\n\n")

	sb.WriteString("## Chấm điểm /10\n")
	sb.WriteString("- 9-10: đăng ngay, cụ thể, cuốn, không lỗi.\n")
	sb.WriteString("- 7-8: đăng được — một biên tập viên kỹ tính vẫn chấp nhận; chỉ còn điểm nhỏ về giọng.\n")
	sb.WriteString("- 5-6: đúng chủ đề nhưng chung chung, thiếu dữ kiện có sẵn, hoặc mở bài yếu.\n")
	sb.WriteString("- 3-4: có dữ kiện bịa, lạc tiêu đề, hoặc đọc như bảng kê.\n")
	sb.WriteString("- 0-2: sai hoàn toàn.\n\n")

	sb.WriteString("## Lỗi có sửa được không\n")
	sb.WriteString("sua_duoc là true nếu người viết sửa được bằng chính nguồn ở trên; false nếu cần thông tin nguồn không có (khi đó chỉ báo, không bắt viết thêm).\n\n")

	sb.WriteString("## Trả lời\n")
	sb.WriteString("Chỉ trả về JSON, không lời dẫn:\n")
	sb.WriteString(`{"ket_luan":"DAT|CANH_BAO","diem":0-10,"loi":[{"loai":"SU_THAT|THIEU_Y|CHUNG_CHUNG|MO_BAI|CTA|META|NGON_NGU","trich_doan":"câu trong bài","chi_tiet":"sai ở đâu và sửa theo hướng nào","sua_duoc":true}],"ghi_chu":"một câu"}`)
	sb.WriteString("\nKhông có lỗi thì ket_luan là DAT và loi là mảng rỗng.\n")
	return sb.String()
}

type contentReviewDefect struct {
	Kind    string `json:"loai"`
	Quote   string `json:"trich_doan"`
	Detail  string `json:"chi_tiet"`
	Fixable *bool  `json:"sua_duoc"`
}

type contentReviewReply struct {
	Verdict string                `json:"ket_luan"`
	Score   *float64              `json:"diem"`
	Defects []contentReviewDefect `json:"loi"`
	Note    string                `json:"ghi_chu"`
}

func failedContentReview(reason string) map[string]any {
	return map[string]any{
		"ket_luan": reviewVerdictWarn,
		"diem":     0,
		"loi": []map[string]any{{
			"loai": "REVIEWER", "trich_doan": "", "chi_tiet": "Người soát không trả được kết quả đọc được.", "sua_duoc": false,
		}},
		"ghi_chu":        "",
		"ly_do_that_bai": reason,
	}
}

// reviewContentReply chuẩn hoá câu trả lời như phía ảnh: fail closed, và số
// liệu được code đối chiếu với nguồn thay vì tin model tự so.
func reviewContentReply(reply string, post map[string]any, source string, facts string) map[string]any {
	object, err := extractJSONObject(reply)
	if err != nil {
		return failedContentReview(err.Error())
	}
	var parsed contentReviewReply
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return failedContentReview(err.Error())
	}

	defects := make([]map[string]any, 0, len(parsed.Defects)+1)
	for _, item := range parsed.Defects {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			continue
		}
		fixable := true
		if item.Fixable != nil {
			fixable = *item.Fixable
		}
		defects = append(defects, map[string]any{
			"loai":       strings.ToUpper(strings.TrimSpace(item.Kind)),
			"trich_doan": strings.TrimSpace(item.Quote),
			"chi_tiet":   detail,
			"sua_duoc":   fixable,
		})
	}

	allowed := strings.Join([]string{stringArg(post, "title"), stringArg(post, "brief"), source, facts}, "\n")
	unsourced := unsourcedNumbers(stringArg(post, "content"), allowed)
	if len(unsourced) > 0 {
		defects = append(defects, map[string]any{
			"loai": "SU_THAT", "trich_doan": "", "sua_duoc": true,
			"chi_tiet": "Số không có trong nguồn: " + strings.Join(unsourced, ", ") + ". Bỏ đi hoặc thay bằng số có trong nguồn.",
		})
	}

	score := 0
	if parsed.Score == nil {
		defects = append(defects, map[string]any{
			"loai": "REVIEWER", "trich_doan": "", "sua_duoc": false, "chi_tiet": "Người soát không chấm điểm.",
		})
	} else {
		score = int(math.Round(math.Max(0, math.Min(10, *parsed.Score))))
	}
	if len(unsourced) > 0 && score > contentCodeDefectMaxScore {
		score = contentCodeDefectMaxScore
	}

	verdict := strings.ToUpper(strings.TrimSpace(parsed.Verdict))
	if verdict != reviewVerdictPass || len(defects) > 0 || score < imageReviewPassScore {
		verdict = reviewVerdictWarn
	}
	if verdict == reviewVerdictWarn && len(defects) == 0 {
		defects = append(defects, map[string]any{
			"loai": "REVIEWER", "trich_doan": "", "sua_duoc": false, "chi_tiet": fmt.Sprintf("Bài mới đạt %d/10, chưa tới %d.", score, imageReviewPassScore),
		})
	}
	return map[string]any{
		"ket_luan": verdict,
		"diem":     score,
		"loi":      defects,
		"ghi_chu":  strings.TrimSpace(parsed.Note),
	}
}

// unsourcedNumbers: số trong bài không xuất hiện trong nguồn (so theo dạng bỏ
// khoảng trắng, "30 %" khớp "30%").
func unsourcedNumbers(content string, allowed string) []string {
	known := map[string]bool{}
	for _, raw := range contentNumberPattern.FindAllString(allowed, -1) {
		known[strings.ReplaceAll(raw, " ", "")] = true
	}
	var missing []string
	seen := map[string]bool{}
	for _, raw := range contentNumberPattern.FindAllString(content, -1) {
		value := strings.ReplaceAll(raw, " ", "")
		if len(value) == 1 || known[value] || seen[value] {
			continue
		}
		seen[value] = true
		missing = append(missing, value)
	}
	return missing
}

func contentReviewNeedsFix(review map[string]any) bool {
	return imageReviewNeedsFix(review)
}

// reviewDraftRounds chấm từng bài của batch, cho người viết sửa theo góp ý khi
// có bài dưới mốc mà còn lỗi sửa được, rồi ghép lại bản điểm cao nhất của mỗi
// bài — bản sửa không chắc tốt hơn bản đầu.
func reviewDraftRounds(
	maxFix int,
	current func() map[string]any,
	review func(post map[string]any) map[string]any,
	revise func(notes string),
) map[string]any {
	type version struct {
		post   map[string]any
		review map[string]any
	}
	best := map[int]version{}
	rounds := 0
	var latest map[string]any
	for attempt := 0; attempt <= maxFix; attempt++ {
		latest = current()
		posts := draftPostList(latest)
		var notes strings.Builder
		for index, post := range posts {
			verdict := review(post)
			if previous, ok := best[index]; !ok || reviewScore(verdict) > reviewScore(previous.review) {
				best[index] = version{post: post, review: verdict}
			}
			if contentReviewNeedsFix(verdict) {
				notes.WriteString(contentFixNotes(index, post, verdict))
			}
		}
		if notes.Len() == 0 || attempt == maxFix {
			break
		}
		revise(notes.String())
		rounds = attempt + 1
	}

	final := make(map[string]any, len(latest))
	for key, value := range latest {
		final[key] = value
	}
	posts := draftPostList(latest)
	merged := make([]map[string]any, 0, len(posts))
	for index, post := range posts {
		chosen, ok := best[index]
		if !ok {
			merged = append(merged, post)
			continue
		}
		out := make(map[string]any, len(chosen.post)+1)
		for key, value := range chosen.post {
			out[key] = value
		}
		reviewCopy := make(map[string]any, len(chosen.review)+1)
		for key, value := range chosen.review {
			reviewCopy[key] = value
		}
		reviewCopy["vong_sua"] = rounds
		out["content_review"] = reviewCopy
		merged = append(merged, out)
	}
	final["posts"] = merged
	return final
}

func draftPostList(batch map[string]any) []map[string]any {
	switch posts := batch["posts"].(type) {
	case []map[string]any:
		return posts
	case []any:
		out := make([]map[string]any, 0, len(posts))
		for _, item := range posts {
			if post, ok := item.(map[string]any); ok {
				out = append(out, post)
			}
		}
		return out
	}
	return nil
}

func contentFixNotes(index int, post map[string]any, review map[string]any) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Bài %d \"%s\" — người soát độc lập chấm %d/10 (mốc là %d). Sửa đúng các lỗi sau, giữ nguyên phần đang tốt:\n", index+1, strings.TrimSpace(stringArg(post, "title")), reviewScore(review), imageReviewPassScore))
	for _, defect := range reviewDefects(review) {
		if fixable, _ := defect["sua_duoc"].(bool); !fixable {
			continue
		}
		line := "- [" + fmt.Sprint(defect["loai"]) + "] "
		if quote := strings.TrimSpace(fmt.Sprint(defect["trich_doan"])); quote != "" && quote != "<nil>" {
			line += "\"" + quote + "\": "
		}
		sb.WriteString(line + fmt.Sprint(defect["chi_tiet"]) + "\n")
	}
	return sb.String()
}

// contentSourceFor tìm dòng checklist mà bài được viết từ (theo source_index).
func contentSourceFor(post map[string]any, items []SourceItem) string {
	index, _ := integerNumberArg(post, "source_index")
	for _, item := range items {
		if item.SourceIndex == index {
			return strings.TrimSpace(strings.Join([]string{item.SourceTitle, item.SourceBrief, item.SourceText}, "\n"))
		}
	}
	return ""
}

// reviewDraftContent nối vòng soát vào lượt viết: người soát là agent chung
// (Drupal chọn), phiên riêng, không thấy suy nghĩ của người viết.
func reviewDraftContent(ctx context.Context, reviewer agent.Agent, writer agent.Agent, first agent.RunRequest, collector *DraftBatchCollectorTool, items []SourceItem, facts string) map[string]any {
	review := func(post map[string]any) map[string]any {
		runID := uuid.NewString()
		result, err := reviewer.Run(ctx, agent.RunRequest{
			SessionKey:    first.SessionKey + ":content-review:" + runID,
			Message:       buildContentReviewPrompt(post, contentSourceFor(post, items), facts),
			Channel:       first.Channel,
			ChannelType:   first.ChannelType,
			ChatID:        first.ChatID,
			PeerKind:      first.PeerKind,
			Addressed:     true,
			RunID:         runID,
			UserID:        first.UserID,
			SenderID:      first.SenderID,
			ToolAllow:     []string{draftWriteNoTools},
			MaxIterations: 1,
			SkillFilter:   []string{},
			LightContext:  true,
			HistoryLimit:  1,
			TraceName:     "tekshot content review",
			TraceTags:     []string{"tekshot", "content_review"},
		})
		if err != nil || result == nil {
			reason := "agent returned no result"
			if err != nil {
				reason = err.Error()
			}
			return failedContentReview(reason)
		}
		return reviewContentReply(result.Content, post, contentSourceFor(post, items), facts)
	}
	revise := func(notes string) {
		req := draftReviewRequest(first)
		req.Message = notes + "\nGọi " + finalToolName + " MỘT lần với cả batch đã sửa. Giữ nguyên title, brief, pillar, checklist_item, source_index, publish_at, publish_date, publish_time; chỉ sửa content và hashtags của bài được nêu. Không bịa: mọi dữ kiện phải có trong dòng checklist hoặc khối RESEARCHED FACTS."
		req.TraceName = "tekshot content revise"
		if _, err := writer.Run(ctx, req); err != nil {
			slog.Warn("tekshot.draft.content_revise_failed", "session", first.SessionKey, "error", err)
		}
	}
	return reviewDraftRounds(contentFixRounds, collector.Batch, review, revise)
}
