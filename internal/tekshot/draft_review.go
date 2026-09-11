package tekshot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// draftReviewIterations covers one turn to judge, one to resubmit and one to
// recover when the collector rejects the resubmission. The editor never
// searches: research already ran as its own pass.
const draftReviewIterations = 3

// buildDraftReviewPrompt asks for an editor's pass over the caption just
// submitted. The writing prompt already carried a self-check, but the model
// submitted in the same turn it wrote, so the check never had a turn to run.
func buildDraftReviewPrompt() string {
	var sb strings.Builder
	sb.WriteString("Đọc lại bài vừa nộp như một biên tập viên giỏi, đối chiếu với dòng checklist và khối RESEARCHED FACTS ở trên.\n\n")
	sb.WriteString("Chấm từng tiêu chí:\n")
	sb.WriteString("1. Tiêu đề hứa điều gì (bước làm, định lượng, ngưỡng, thông số, ví dụ) mà dòng checklist hoặc khối RESEARCHED FACTS có nhưng bài chưa dùng? Đưa vào. Ngược lại, bỏ chi tiết vặt không giúp người đọc làm hay hiểu điều đó (calo, đời máy, chú thích thời gian).\n")
	sb.WriteString("2. Có câu nào đặt vào bài của page khác vẫn đúng không (tả không khí, cảm xúc chung chung, lời khen sáo)? Viết lại cho cụ thể hoặc xoá.\n")
	sb.WriteString("3. Câu nào không làm việc gì (không mang dữ kiện, không mở bài, không nối ý, không kêu gọi)? Xoá. Giữ nguyên mọi dữ kiện đã tra được — đó là nguyên liệu, không phải câu độn.\n")
	sb.WriteString("4. 3 dòng đầu có câu mở bắt đúng vấn đề hoặc kết quả mà tiêu đề hứa không?\n")
	sb.WriteString("5. Có tính từ nào gắn vào dữ kiện chỉ để kéo dài câu không? Bỏ.\n")
	sb.WriteString("6. Bài có đọc như một bài Facebook do người viết giỏi viết (có mở bài, có nối ý, có kêu gọi) hay như một bảng kê thông số? Nếu là bảng kê, viết lại cho có giọng mà không thêm câu sáo.\n")
	sb.WriteString("7. Bài có câu nào nhắc tới nguồn, checklist, việc tra cứu, thông tin còn thiếu, hay bàn chuyện số liệu lệch nhau không? Xoá — người đọc chỉ thấy bài đăng; khi lệch, giữ số của dòng checklist.\n")
	sb.WriteString("8. Dữ kiện tra từ nguồn nước ngoài đã đổi sang đơn vị và từ ngữ của người đọc Việt chưa (g, ml, muỗng canh, °C; không cup, tablespoon, °F, không để nguyên từ tiếng Anh)?\n\n")
	sb.WriteString("Nếu mọi tiêu chí đều đạt: trả lời đúng một chữ ĐẠT và không gọi tool nào.\n")
	sb.WriteString("Nếu có tiêu chí chưa đạt: gọi " + finalToolName + " MỘT lần với bài đã sửa. Giữ nguyên title, brief, pillar, checklist_item, source_index, publish_at, publish_date, publish_time của lần nộp trước; chỉ sửa content và hashtags.\n")
	sb.WriteString("Không bịa: mọi dữ kiện phải có trong dòng checklist hoặc khối RESEARCHED FACTS.\n")
	return sb.String()
}

func draftReviewRequest(first agent.RunRequest) agent.RunRequest {
	req := first
	req.RunID = uuid.NewString()
	req.Message = buildDraftReviewPrompt()
	req.MaxIterations = draftReviewIterations
	req.ToolAllow = []string{draftWriteNoTools}
	req.ToolChoice = nil
	req.TraceName = "tekshot draft review"
	return req
}

// runDraftReview is best-effort: a failed or silent review keeps the first
// submission, because the collector only overwrites on a valid resubmit.
func runDraftReview(ctx context.Context, ag agent.Agent, first agent.RunRequest, collector *DraftBatchCollectorTool) {
	before := collector.Batch()
	if before == nil {
		return
	}
	if _, err := ag.Run(ctx, draftReviewRequest(first)); err != nil {
		slog.Warn("tekshot.draft.review_failed", "session", first.SessionKey, "error", err)
		return
	}
	slog.Info("tekshot.draft.reviewed", "session", first.SessionKey, "revised", draftContentChanged(before, collector.Batch()))
}

func draftContentChanged(before, after map[string]any) bool {
	return draftContents(before) != draftContents(after)
}

// draftContents accepts both shapes: the collector stores []map[string]any,
// while a batch decoded from JSON carries []any.
func draftContents(batch map[string]any) string {
	var parts []string
	switch posts := batch["posts"].(type) {
	case []map[string]any:
		for _, post := range posts {
			parts = append(parts, stringArg(post, "content"))
		}
	case []any:
		for _, post := range posts {
			if entry, ok := post.(map[string]any); ok {
				parts = append(parts, stringArg(entry, "content"))
			}
		}
	}
	return strings.Join(parts, "\x00")
}
