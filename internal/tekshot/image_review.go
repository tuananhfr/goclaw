package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	TekshotJobTypeImageReview = "image_review"

	reviewVerdictPass = "DAT"
	reviewVerdictWarn = "CANH_BAO"

	imageReviewMaxIterations = 4
	imageReviewMinTextRunes  = 2
)

// imageReviewToolAllow: người soát chỉ được NHÌN ảnh. read_image phải có vì ở
// chế độ file-ref model chính không thấy ảnh inline (agent/loop_input_media.go).
func imageReviewToolAllow() []string {
	return []string{"read_image"}
}

// runImageReview soát ảnh final của một bài trong phiên riêng: không thấy
// prompt, lịch sử hay lý lẽ của lượt sinh ảnh — chỉ thấy bài, nguồn và ảnh.
// Tự soát ảnh của chính mình đã cho qua cả biển hiệu bịa chữ.
func (s *JobService) runImageReview(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	mediaFiles := mediaFromJobRequest(request)
	if len(mediaFiles) == 0 {
		return nil, "", fmt.Errorf("image_review requires the uploaded final image")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	infos := make([]media.MediaInfo, 0, len(mediaFiles))
	for _, item := range mediaFiles {
		mimeType := strings.TrimSpace(item.MimeType)
		if mimeType == "" {
			mimeType = media.DetectMIMEType(item.Path)
		}
		infos = append(infos, media.MediaInfo{Type: media.TypeImage, FilePath: item.Path, ContentType: mimeType, FileName: item.Filename})
	}
	message := buildImageReviewPrompt(request)
	if tags := media.BuildMediaTags(infos); tags != "" {
		message = tags + "\n\n" + message
	}

	result, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:    job.SessionKey + ":review",
		Message:       message,
		Media:         mediaFiles,
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     imageReviewToolAllow(),
		MaxIterations: imageReviewMaxIterations,
		SkillFilter:   []string{},
		LightContext:  true,
		HistoryLimit:  1,
		TraceName:     "tekshot image review",
		TraceTags:     []string{"tekshot", "image_review"},
	})
	if err != nil {
		return nil, "", err
	}
	reply := ""
	if result != nil {
		reply = result.Content
	}
	review := reviewImageReply(reply, imageReviewPostText(request))
	return review, "Image reviewed: " + review["ket_luan"].(string), nil
}

func buildImageReviewPrompt(request map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Bạn là người soát ảnh ĐỘC LẬP cho một bài Facebook. Bạn không tạo ra ảnh này và không biết người tạo định vẽ gì — chỉ đánh giá cái thực sự nhìn thấy, đối chiếu với bài.\n")
	sb.WriteString("Ảnh được đính kèm tin nhắn này; nếu không thấy ảnh trực tiếp, hãy đọc nó bằng read_image trước khi kết luận.\n\n")

	sb.WriteString("## Page\n")
	sb.WriteString("- Tên: " + strings.TrimSpace(stringFromMap(request, "page_name")) + "\n")
	sb.WriteString("- Mô tả: " + strings.TrimSpace(stringFromMap(request, "page_description")) + "\n\n")

	sb.WriteString("## Bài viết (nguồn sự thật duy nhất)\n")
	sb.WriteString("- Tiêu đề: " + strings.TrimSpace(stringFromMap(request, "post_title")) + "\n")
	sb.WriteString("- Brief: " + strings.TrimSpace(stringFromMap(request, "post_brief")) + "\n")
	sb.WriteString("- Nội dung:\n" + strings.TrimSpace(stringFromMap(request, "post_content")) + "\n")
	if row := strings.TrimSpace(stringFromMap(request, "source_row")); row != "" {
		sb.WriteString("- Dòng checklist nguồn:\n" + row + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Soát theo đúng 6 mục, chỉ báo lỗi THẬT, không báo gu thẩm mỹ\n")
	sb.WriteString("- THONG_DIEP: ảnh không thể hiện ý chính của bài, hoặc lạc đề.\n")
	sb.WriteString("- CHU: chữ trong ảnh sai chính tả tiếng Việt, có emoji, hoặc có dấu ngoặc kép bị vẽ ra.\n")
	sb.WriteString("- PHUONG_TIEN: cách thể hiện trái với chủ thể — ví dụ đồ ăn hoặc sản phẩm bị vẽ hoạt hình, phác thảo thay vì ảnh thật hấp dẫn.\n")
	sb.WriteString("- SAN_PHAM: ảnh vẽ sản phẩm của page với chi tiết bài không nói (thành phần, hình dáng, bao bì, logo thương hiệu khác) nên dễ gây hiểu nhầm cho khách.\n")
	sb.WriteString("- NOI_QUA: ảnh thể hiện con số, lời hứa, tính năng hay kết quả mà bài không có.\n")
	sb.WriteString("- KY_THUAT: chủ thể méo, tay hoặc mặt lỗi, vật thể vô lý, chữ vỡ không đọc được.\n\n")

	sb.WriteString("## Chép lại chữ\n")
	sb.WriteString("Liệt kê vào chu_trong_anh MỌI cụm chữ đọc được trong ảnh — cả chữ trên biển hiệu, bao bì, poster, màn hình — đúng như nó xuất hiện, mỗi cụm một phần tử. Chép đúng từng ký tự: giữ nguyên dấu ngoặc kép, emoji và ký hiệu nếu chúng được vẽ trong ảnh, không tự làm sạch. Không có chữ nào thì để mảng rỗng. Việc so chữ với bài do hệ thống làm, bạn chỉ cần chép đúng.\n\n")

	sb.WriteString("## Trả lời\n")
	sb.WriteString("Chỉ trả về JSON, không lời dẫn:\n")
	sb.WriteString(`{"ket_luan":"DAT|CANH_BAO","chu_trong_anh":["..."],"loi":[{"loai":"THONG_DIEP|CHU|PHUONG_TIEN|SAN_PHAM|NOI_QUA|KY_THUAT","vi_tri":"vùng nào trong ảnh","chi_tiet":"thấy gì và sai ở đâu so với bài"}],"ghi_chu":"một câu"}`)
	sb.WriteString("\nKhông có lỗi thì ket_luan là DAT và loi là mảng rỗng. Có bất kỳ lỗi nào thì ket_luan là CANH_BAO.\n")
	return sb.String()
}

// imageReviewPostText là toàn bộ chữ mà ảnh được phép mượn.
func imageReviewPostText(request map[string]any) string {
	return strings.Join([]string{
		stringFromMap(request, "post_title"),
		stringFromMap(request, "post_brief"),
		stringFromMap(request, "post_content"),
		stringFromMap(request, "source_row"),
	}, "\n")
}

type imageReviewDefect struct {
	Kind   string `json:"loai"`
	Where  string `json:"vi_tri"`
	Detail string `json:"chi_tiet"`
}

type imageReviewReply struct {
	Verdict string              `json:"ket_luan"`
	Text    []string            `json:"chu_trong_anh"`
	Defects []imageReviewDefect `json:"loi"`
	Note    string              `json:"ghi_chu"`
}

// reviewImageReply chuẩn hoá câu trả lời và fail closed: không đọc được, kết
// luận lạ, hay kết luận mâu thuẫn danh sách lỗi đều thành CANH_BAO. Chữ trong
// ảnh được so với bài bằng code, không tin model tự so.
func reviewImageReply(reply string, postText string) map[string]any {
	parsed, err := parseImageReviewReply(reply)
	if err != nil {
		return map[string]any{
			"ket_luan":      reviewVerdictWarn,
			"chu_trong_anh": []string{},
			"loi": []map[string]any{{
				"loai": "REVIEWER", "vi_tri": "", "chi_tiet": "Người soát không trả được kết quả đọc được.",
			}},
			"ghi_chu":        "",
			"ly_do_that_bai": err.Error(),
		}
	}

	defects := make([]map[string]any, 0, len(parsed.Defects)+len(parsed.Text))
	for _, item := range parsed.Defects {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			continue
		}
		defects = append(defects, map[string]any{
			"loai":     strings.ToUpper(strings.TrimSpace(item.Kind)),
			"vi_tri":   strings.TrimSpace(item.Where),
			"chi_tiet": detail,
		})
	}

	// Gộp thành một cảnh báo mỗi loại: người soát hay chép biển hiệu từng từ
	// một, tách dòng thì một tấm biển thành bốn cảnh báo.
	seen := make([]string, 0, len(parsed.Text))
	var marked, missing []string
	normalizedPost := normalizeReviewText(postText)
	for _, raw := range parsed.Text {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		seen = append(seen, text)
		if hasDrawnMarks(text) {
			marked = append(marked, text)
		}
		key := normalizeReviewText(text)
		if len([]rune(strings.TrimSpace(key))) < imageReviewMinTextRunes || strings.Contains(normalizedPost, key) {
			continue
		}
		missing = append(missing, fmt.Sprintf("%q", text))
	}
	if len(marked) > 0 {
		defects = append(defects, map[string]any{
			"loai":     "CHU",
			"vi_tri":   "",
			"chi_tiet": "Vẽ cả emoji hoặc dấu ngoặc kép: " + strings.Join(marked, " / "),
		})
	}
	if len(missing) > 0 {
		defects = append(defects, map[string]any{
			"loai":     "CHU",
			"vi_tri":   "",
			"chi_tiet": "Không có trong bài: " + strings.Join(missing, ", ") + ".",
		})
	}

	verdict := strings.ToUpper(strings.TrimSpace(parsed.Verdict))
	if verdict != reviewVerdictPass || len(defects) > 0 {
		verdict = reviewVerdictWarn
	}
	if verdict == reviewVerdictWarn && len(defects) == 0 {
		defects = append(defects, map[string]any{
			"loai": "REVIEWER", "vi_tri": "", "chi_tiet": "Người soát kết luận cảnh báo nhưng không nêu lỗi cụ thể.",
		})
	}
	return map[string]any{
		"ket_luan":      verdict,
		"chu_trong_anh": seen,
		"loi":           defects,
		"ghi_chu":       strings.TrimSpace(parsed.Note),
	}
}

func parseImageReviewReply(reply string) (*imageReviewReply, error) {
	object, err := extractJSONObject(reply)
	if err != nil {
		return nil, err
	}
	var parsed imageReviewReply
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return nil, fmt.Errorf("image review reply is not valid JSON: %w", err)
	}
	return &parsed, nil
}

// hasDrawnMarks: emoji hay ngoặc kép bị model sinh ảnh vẽ nguyên văn — lỗi
// dù chữ bên trong có trong bài (ảnh khuôn tiêu đề cũ vẽ cả 📊 và "…").
func hasDrawnMarks(text string) bool {
	for _, r := range text {
		switch {
		case r == '"' || r == '“' || r == '”' || r == '„' || r == '«' || r == '»':
			return true
		case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF, r >= 0x2B00 && r <= 0x2BFF:
			return true
		}
	}
	return false
}

// normalizeReviewText: NFC (dấu tiếng Việt lúc dựng sẵn lúc tổ hợp), chữ
// thường, bỏ dấu câu, gộp khoảng trắng — "ĐO LƯỜNG HIỆU QUẢ!" vẫn khớp bài.
func normalizeReviewText(text string) string {
	var sb strings.Builder
	space := false
	for _, r := range strings.ToLower(norm.NFC.String(text)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			sb.WriteRune(r)
			space = false
			continue
		}
		if !space {
			sb.WriteRune(' ')
			space = true
		}
	}
	return " " + strings.TrimSpace(sb.String()) + " "
}
