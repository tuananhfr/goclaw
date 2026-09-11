package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	TekshotJobTypeImageReview = "image_review"

	reviewVerdictPass = "DAT"
	reviewVerdictWarn = "CANH_BAO"

	// imageReviewPassScore: mốc "người duyệt kỹ tính vẫn cho đăng".
	imageReviewPassScore = 7
	// Lỗi do code bắt (chữ bịa, emoji/ngoặc kép) kéo điểm xuống dưới mốc để
	// vòng sửa chắc chắn chạy — model hay cho 8-9 dù ảnh có biển hiệu bịa.
	imageReviewCodeDefectMaxScore = 5

	imageReviewMaxIterations = 4
	imageReviewMinTextRunes  = 2
)

// imageReviewToolAllow: người soát chỉ được NHÌN ảnh. read_image phải có vì ở
// chế độ file-ref model chính không thấy ảnh inline (agent/loop_input_media.go).
func imageReviewToolAllow() []string {
	return []string{"read_image"}
}

// runImageReview soát ảnh final của một bài (ảnh chat tay, hoặc ảnh cron khi
// GoClaw cũ chưa soát trong vòng sinh). Agent do Drupal chọn trong cài đặt.
func (s *JobService) runImageReview(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	mediaFiles := mediaFromJobRequest(request)
	if len(mediaFiles) == 0 {
		return nil, "", fmt.Errorf("image_review requires the uploaded final image")
	}
	review := s.reviewImage(ctx, job, job.AgentKey, request, mediaFiles)
	return review, fmt.Sprintf("Image reviewed: %d/10", review["diem"]), nil
}

// reviewImage chạy một phiên soát riêng: không thấy prompt, lịch sử hay lý lẽ
// của lượt sinh ảnh — chỉ thấy bài, nguồn và ảnh. Tự soát ảnh của chính mình
// đã cho qua cả biển hiệu bịa chữ. Lỗi hạ tầng trả về kết luận fail closed.
func (s *JobService) reviewImage(ctx context.Context, job *store.TekshotJob, agentKey string, request map[string]any, mediaFiles []bus.MediaFile) map[string]any {
	if s.agents == nil {
		return failedImageReview("agent router is not configured")
	}
	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), agentKey)
	if err != nil {
		return failedImageReview("không gọi được agent soát ảnh: " + err.Error())
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, agentKey)

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

	runID := uuid.NewString()
	result, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:    job.SessionKey + ":review:" + runID,
		Message:       message,
		Media:         mediaFiles,
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         runID,
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
		return failedImageReview(err.Error())
	}
	reply := ""
	if result != nil {
		reply = result.Content
	}
	return reviewImageReply(reply, imageReviewPostText(request))
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
	sb.WriteString("- KY_THUAT: chủ thể méo, tay hoặc mặt lỗi, vật thể vô lý, chữ vỡ không đọc được.\n")
	sb.WriteString("Không trừ điểm vì ảnh không có chữ: ảnh chỉ bằng hình vẫn có thể đạt điểm cao.\n\n")

	sb.WriteString("## Chấm điểm /10\n")
	sb.WriteString("- 9-10: đăng ngay, nổi bật, không lỗi.\n")
	sb.WriteString("- 7-8: đăng được — một người duyệt kỹ tính vẫn chấp nhận; chỉ còn điểm nhỏ về gu.\n")
	sb.WriteString("- 5-6: đúng chủ đề nhưng có lỗi nhìn thấy ngay (chữ sai, chi tiết vô lý, thông điệp mờ).\n")
	sb.WriteString("- 3-4: lạc đề một phần, chữ bịa, hoặc cách thể hiện trái chủ thể.\n")
	sb.WriteString("- 0-2: sai hoàn toàn hoặc ảnh hỏng.\n\n")

	sb.WriteString("## Lỗi có sửa được không\n")
	sb.WriteString("Với mỗi lỗi, sua_duoc là true nếu sinh lại ảnh có thể sửa được (chữ, bố cục, thông điệp, cách thể hiện, lỗi kỹ thuật); false nếu phải có thứ máy không tạo được — ảnh chụp thật, ảnh sản phẩm thật, hoặc thông tin bài không có.\n\n")

	sb.WriteString("## Chép lại chữ\n")
	sb.WriteString("Liệt kê vào chu_trong_anh MỌI cụm chữ đọc được trong ảnh — cả chữ trên biển hiệu, bao bì, poster, màn hình — đúng như nó xuất hiện, mỗi cụm một phần tử. Chép đúng từng ký tự: giữ nguyên dấu ngoặc kép, emoji và ký hiệu nếu chúng được vẽ trong ảnh, không tự làm sạch. Không có chữ nào thì để mảng rỗng. Việc so chữ với bài do hệ thống làm, bạn chỉ cần chép đúng.\n\n")

	sb.WriteString("## Trả lời\n")
	sb.WriteString("Chỉ trả về JSON, không lời dẫn:\n")
	sb.WriteString(`{"ket_luan":"DAT|CANH_BAO","diem":0-10,"chu_trong_anh":["..."],"loi":[{"loai":"THONG_DIEP|CHU|PHUONG_TIEN|SAN_PHAM|NOI_QUA|KY_THUAT","vi_tri":"vùng nào trong ảnh","chi_tiet":"thấy gì và sai ở đâu so với bài","sua_duoc":true}],"ghi_chu":"một câu"}`)
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
	Kind    string `json:"loai"`
	Where   string `json:"vi_tri"`
	Detail  string `json:"chi_tiet"`
	Fixable *bool  `json:"sua_duoc"`
}

type imageReviewReply struct {
	Verdict string              `json:"ket_luan"`
	Score   *float64            `json:"diem"`
	Text    []string            `json:"chu_trong_anh"`
	Defects []imageReviewDefect `json:"loi"`
	Note    string              `json:"ghi_chu"`
}

// failedImageReview: không soát được thì không phải là đạt, và cũng không phải
// lý do để sinh lại ảnh.
func failedImageReview(reason string) map[string]any {
	return map[string]any{
		"ket_luan":      reviewVerdictWarn,
		"diem":          0,
		"chu_trong_anh": []string{},
		"loi": []map[string]any{{
			"loai": "REVIEWER", "vi_tri": "", "chi_tiet": "Người soát không trả được kết quả đọc được.", "sua_duoc": false,
		}},
		"ghi_chu":        "",
		"ly_do_that_bai": reason,
	}
}

// reviewImageReply chuẩn hoá câu trả lời và fail closed: không đọc được, không
// chấm điểm, kết luận lạ, hay kết luận mâu thuẫn danh sách lỗi đều thành
// CANH_BAO. Chữ trong ảnh được so với bài bằng code, không tin model tự so.
func reviewImageReply(reply string, postText string) map[string]any {
	parsed, err := parseImageReviewReply(reply)
	if err != nil {
		return failedImageReview(err.Error())
	}

	defects := make([]map[string]any, 0, len(parsed.Defects)+2)
	for _, item := range parsed.Defects {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			continue
		}
		kind := strings.ToUpper(strings.TrimSpace(item.Kind))
		fixable := defaultFixable(kind)
		if item.Fixable != nil {
			fixable = *item.Fixable
		}
		defects = append(defects, map[string]any{
			"loai":     kind,
			"vi_tri":   strings.TrimSpace(item.Where),
			"chi_tiet": detail,
			"sua_duoc": fixable,
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
			"loai": "CHU", "vi_tri": "", "sua_duoc": true,
			"chi_tiet": "Vẽ cả emoji hoặc dấu ngoặc kép: " + strings.Join(marked, " / "),
		})
	}
	if len(missing) > 0 {
		defects = append(defects, map[string]any{
			"loai": "CHU", "vi_tri": "", "sua_duoc": true,
			"chi_tiet": "Không có trong bài: " + strings.Join(missing, ", ") + ".",
		})
	}

	score := 0
	if parsed.Score == nil {
		defects = append(defects, map[string]any{
			"loai": "REVIEWER", "vi_tri": "", "sua_duoc": false, "chi_tiet": "Người soát không chấm điểm.",
		})
	} else {
		score = int(math.Round(math.Max(0, math.Min(10, *parsed.Score))))
	}
	if (len(marked) > 0 || len(missing) > 0) && score > imageReviewCodeDefectMaxScore {
		score = imageReviewCodeDefectMaxScore
	}

	verdict := strings.ToUpper(strings.TrimSpace(parsed.Verdict))
	if verdict != reviewVerdictPass || len(defects) > 0 || score < imageReviewPassScore {
		verdict = reviewVerdictWarn
	}
	if verdict == reviewVerdictWarn && len(defects) == 0 {
		defects = append(defects, map[string]any{
			"loai": "REVIEWER", "vi_tri": "", "sua_duoc": false, "chi_tiet": fmt.Sprintf("Ảnh mới đạt %d/10, chưa tới %d.", score, imageReviewPassScore),
		})
	}
	return map[string]any{
		"ket_luan":      verdict,
		"diem":          score,
		"chu_trong_anh": seen,
		"loi":           defects,
		"ghi_chu":       strings.TrimSpace(parsed.Note),
	}
}

// defaultFixable khi model không nói: sản phẩm thật và lỗi của chính người soát
// thì sinh lại không cứu được; còn lại coi là sửa được.
func defaultFixable(kind string) bool {
	return kind != "SAN_PHAM" && kind != "REVIEWER"
}

// imageReviewNeedsFix: chỉ sinh lại khi dưới mốc VÀ còn ít nhất một lỗi sinh
// lại sửa được. Chỉ có lỗi "cần ảnh thật" thì thông báo, không đốt thêm lượt.
func imageReviewNeedsFix(review map[string]any) bool {
	if reviewScore(review) >= imageReviewPassScore {
		return false
	}
	for _, defect := range reviewDefects(review) {
		if fixable, _ := defect["sua_duoc"].(bool); fixable {
			return true
		}
	}
	return false
}

// imageReviewFixNotes là góp ý gửi lại cho lượt sinh sau: điểm và các lỗi sửa
// được, không kèm lỗi máy không sửa được.
func imageReviewFixNotes(review map[string]any) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("An independent reviewer scored the previous image %d/10 (the bar is %d). Keep what works and fix exactly these defects:\n", reviewScore(review), imageReviewPassScore))
	for _, defect := range reviewDefects(review) {
		if fixable, _ := defect["sua_duoc"].(bool); !fixable {
			continue
		}
		where := strings.TrimSpace(fmt.Sprint(defect["vi_tri"]))
		line := "- [" + fmt.Sprint(defect["loai"]) + "] "
		if where != "" {
			line += where + ": "
		}
		sb.WriteString(line + fmt.Sprint(defect["chi_tiet"]) + "\n")
	}
	return sb.String()
}

func reviewScore(review map[string]any) int {
	switch value := review["diem"].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}

func reviewDefects(review map[string]any) []map[string]any {
	if defects, ok := review["loi"].([]map[string]any); ok {
		return defects
	}
	return nil
}

// firstMediaPath đọc ảnh đầu tiên từ kết quả runner — kiểu gốc hoặc đã decode.
func firstMediaPath(result map[string]any) string {
	switch items := result["media"].(type) {
	case []agent.MediaResult:
		for _, item := range items {
			if strings.TrimSpace(item.Path) != "" {
				return item.Path
			}
		}
	case []any:
		for _, item := range items {
			if record, ok := item.(map[string]any); ok {
				if path := strings.TrimSpace(stringFromMap(record, "path")); path != "" {
					return path
				}
			}
		}
	}
	return ""
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
