package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// Prompt D — overlay tuân thủ.
//
// Lớp kiểm tra THỨ HAI, tách khỏi lượt viết để không lẫn với việc viết: cùng
// một lượt vừa viết vừa tự soát thì nó soát bằng đúng cái điểm mù đã viết ra
// bài. Chạy khi muc_dich != THONG_TIN.
const (
	verdictAllow = "DUOC_DANG"
	verdictFix   = "CAN_SUA"
	verdictBlock = "CHAN"

	// Tool-free như reference_choice và knowledge_screen.
	complianceNoTools = "compliance/no-tools"
	complianceTimeout = 90 * time.Second
)

// complianceChecks là C1–C22 của Bộ Prompt v3.0, gắn nhãn profile áp dụng.
// Rỗng = áp cho mọi profile.
var complianceChecks = []struct {
	Code     string
	Profiles []string
	Rule     string
}{
	{"C1", nil, "Nhãn quảng cáo/tài trợ có ở DÒNG ĐẦU và trong 125 ký tự đầu? Đặt cuối bài hoặc trong bình luận = KHÔNG ĐẠT."},
	{"C2", nil, "Mọi khẳng định về sản phẩm, giá, khuyến mại, kỹ thuật, năng lực, thu nhập có nguồn còn hiệu lực?"},
	{"C3", nil, "Có từ tuyệt đối (nhất/số một/duy nhất/tốt nhất/hàng đầu/đầu tiên) mà không có tài liệu? → BỎ TỪ."},
	{"C4", nil, "Có so sánh trực tiếp, xếp hạng, hoặc nêu tên cơ sở kinh doanh ngoài hệ sinh thái?"},
	{"C5", nil, "Có nêu công dụng chữa bệnh, tác dụng sức khỏe, GIÁ TRỊ DINH DƯỠNG, hoặc MỨC ĐỘ PHÙ HỢP VỚI NHÓM ĐỐI TƯỢNG cho thực phẩm? → CHẶN, không ngoại lệ."},
	{"C6", []string{"P2"}, "Sản phẩm thuộc diện phải đăng ký nội dung quảng cáo trước mà chưa đăng ký? → CHẶN."},
	{"C7", []string{"P2"}, "Nội dung có vượt ra ngoài công dụng, đối tượng, cảnh báo trong bản công bố?"},
	{"C8", []string{"P3"}, "Đã thực sự dùng hoặc hiểu rõ sản phẩm này chưa, và đã kiểm tra tài liệu pháp lý của bên quảng cáo chưa? Chưa → CHẶN."},
	{"C9", []string{"P3", "P6"}, "Mọi tên, logo, nhãn hiệu, hình ảnh bên thứ ba trong bài có số văn bản đồng ý còn hiệu lực? Không → CHẶN."},
	{"C10", []string{"P4", "P6"}, "Có lời chứng, phát ngôn gán cho người thật thiếu văn bản xác nhận, hoặc chữ mẫu chưa xóa? → CHẶN."},
	{"C11", []string{"P4"}, "Có con số về hiệu quả đầu tư, mức tiết kiệm, hoàn vốn, doanh thu mà không có cơ sở tính toán, HOẶC không ghi điều kiện tính ngay tại chỗ nêu số? → CHẶN."},
	{"C12", []string{"P4"}, "Có khối \"Lưu ý — không phải cam kết kết quả kinh doanh\" và khối \"Xử lý thông tin đăng ký\"? Thiếu → CHẶN."},
	{"C13", []string{"P4"}, "Có tạo sức ép thời hạn hoặc khan hiếm mà không có căn cứ? → CHẶN."},
	{"C14", []string{"P5"}, "Số liệu kỹ thuật có ghi điều kiện áp dụng? Có dải số nào mâu thuẫn không?"},
	{"C15", []string{"P5"}, "Có nêu tên, ảnh, giá trị hợp đồng công trình mà không có dòng \"đồng ý công bố\"? → CHẶN."},
	{"C16", []string{"P5"}, "Có nêu đích danh cơ sở khác và quy kết làm hàng giả, hàng nhái, kém chất lượng khi chưa có kết luận cơ quan chức năng? → CHẶN."},
	{"C17", []string{"P6"}, "Có mô tả tính năng sinh trắc học (nhận diện khuôn mặt, chấm công bằng khuôn mặt, nhận diện khách quen, nhận diện biển số) mà KHÔNG nêu điều kiện triển khai về cơ sở pháp lý xử lý dữ liệu? → CHẶN."},
	{"C18", []string{"P6"}, "Có số liệu hiệu năng mô hình AI không kèm điều kiện đo và kỳ đo? → CHẶN."},
	{"C19", []string{"P7"}, "Có yêu cầu/ưu tiên theo giới tính, tuổi, hôn nhân, thai sản, dân tộc, tôn giáo, quê quán, khuyết tật, ngoại hình — kể cả gián tiếp? → CHẶN."},
	{"C20", []string{"P7"}, "Có yêu cầu ứng viên nộp tiền, đặt cọc, giữ giấy tờ gốc? → CHẶN."},
	{"C21", []string{"P7"}, "Có câu thông báo xử lý dữ liệu ứng viên nêu thời hạn lưu và cách yêu cầu xóa? Không có → CHẶN."},
	{"C22", []string{"P7"}, "Pháp nhân tuyển dụng ghi trong bài có khớp pháp nhân của trang? Hệ sinh thái nhiều pháp nhân — ghi nhầm là chặn."},
}

var complianceWarnings = []struct {
	Code string
	Rule string
}{
	{"W1", "Khối giới hạn thông tin có ghi thời hạn và điều kiện áp dụng?"},
	{"W2", "Bài đã đặt lịch rà soát cho ngày hết hạn (khuyến mại / nguồn / uỷ quyền nhãn hiệu)?"},
	{"W3", "Có dữ kiện nào trong bài mâu thuẫn với một nguồn khác?"},
	{"W4", "[Thực phẩm bảo vệ sức khỏe] Khuyến cáo bắt buộc đã chép NGUYÊN VĂN?"},
	{"W5", "Địa chỉ, tên đơn vị hành chính trong bài có khớp nguồn?"},
}

// applicableChecks lọc C theo profile của trang.
func applicableChecks(codes []string) []string {
	set := make(map[string]bool, len(codes))
	for _, code := range codes {
		set[strings.ToUpper(strings.TrimSpace(code))] = true
	}
	out := make([]string, 0, len(complianceChecks))
	for _, check := range complianceChecks {
		if len(check.Profiles) == 0 {
			out = append(out, check.Code+". "+check.Rule)
			continue
		}
		for _, profile := range check.Profiles {
			if set[profile] {
				out = append(out, check.Code+". "+check.Rule)
				break
			}
		}
	}
	return out
}

func buildCompliancePrompt(profile *pageProfile, post map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Bạn nhận một bài đã soạn xong. Nhiệm vụ KHÔNG phải viết lại bài, mà là kiểm tra nghĩa vụ và trả về danh sách sửa bắt buộc.\n")
	sb.WriteString("Bạn KHÔNG được tự sửa bài. Chỉ nêu chỗ sai và cách sửa. Người quyết định.\n\n")

	sb.WriteString("## BÀI CẦN SOÁT\n")
	sb.WriteString("Tiêu đề: " + stringFromMap(post, "title") + "\n")
	sb.WriteString("Nhãn tuân thủ đang đặt: " + orDash(stringFromMap(post, "khoi_7_tuan_thu")) + "\n")
	sb.WriteString("Khối giới hạn thông tin: " + orDash(stringFromMap(post, "khoi_4_gioi_han")) + "\n")
	sb.WriteString("Tự chấm rủi ro: " + orDash(stringFromMap(post, "tu_cham_risk")) + " — lý do: " + orDash(stringFromMap(post, "ly_do_cham_risk")) + "\n")
	if claims := stringSliceFromAny(post["claim_khong_co_nguon"]); len(claims) > 0 {
		sb.WriteString("Khẳng định KHÔNG có nguồn (do chính lượt viết khai): " + strings.Join(claims, "; ") + "\n")
	}
	sb.WriteString("\nNội dung:\n")
	sb.WriteString(stringFromMap(post, "content"))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("## NGỮ CẢNH TRANG\nProfile: %s. Pháp nhân: %s.\n",
		strings.Join(profile.Codes, " + "), orDash(profile.LegalEntity)))
	if len(profile.Forbidden) > 0 {
		sb.WriteString("Cấm riêng của trang:\n")
		for _, rule := range profile.Forbidden {
			sb.WriteString("- " + rule + "\n")
		}
	}

	sb.WriteString("\n## NHÓM CHẶN\n")
	for _, line := range applicableChecks(profile.Codes) {
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n## NHÓM CẢNH BÁO\n")
	for _, warning := range complianceWarnings {
		sb.WriteString(warning.Code + ". " + warning.Rule + "\n")
	}

	sb.WriteString("\n## ĐẦU RA\n")
	sb.WriteString("Trả lời CHỈ bằng một object JSON:\n")
	sb.WriteString(`{"ket_luan":"DUOC_DANG|CAN_SUA|CHAN",`)
	sb.WriteString(`"loi_chan":[{"ma":"C1","trich_doan":"","sua_the_nao":""}],`)
	sb.WriteString(`"canh_bao":[{"ma":"W1","noi_dung":""}],`)
	sb.WriteString(`"lich_ra_soat_de_xuat":{"ngay":"YYYY-MM-DD","ly_do":""}}` + "\n\n")
	sb.WriteString("Có bất kỳ lỗi chặn nào → ket_luan phải là CHAN. Chỉ có cảnh báo → CAN_SUA. Sạch cả hai → DUOC_DANG.\n")
	sb.WriteString("`trich_doan` chép NGUYÊN VĂN đoạn trong bài gây ra lỗi, không diễn giải lại.\n")
	sb.WriteString("Chỉ gắn lỗi khi bài THỰC SỰ vi phạm. Một cổng chặn nhầm mọi thứ thì người duyệt sẽ bỏ qua nó.\n")
	sb.WriteString("Không gọi bất kỳ tool nào. Trả lời bằng JSON dạng văn bản thuần.\n")
	return sb.String()
}

type complianceBlocker struct {
	Code    string `json:"ma"`
	Excerpt string `json:"trich_doan"`
	Fix     string `json:"sua_the_nao"`
}

type complianceWarning struct {
	Code    string `json:"ma"`
	Content string `json:"noi_dung"`
}

type complianceReply struct {
	Verdict  string              `json:"ket_luan"`
	Blockers []complianceBlocker `json:"loi_chan"`
	Warnings []complianceWarning `json:"canh_bao"`
	Review   struct {
		Date   string `json:"ngay"`
		Reason string `json:"ly_do"`
	} `json:"lich_ra_soat_de_xuat"`
}

// normalizeVerdict ép về ba giá trị. Có lỗi chặn thì luôn là CHAN, dù model
// tự kết luận nhẹ hơn — kết luận không được mâu thuẫn với chính danh sách nó
// vừa liệt kê.
func normalizeVerdict(reply *complianceReply) string {
	if len(reply.Blockers) > 0 {
		return verdictBlock
	}
	switch strings.ToUpper(strings.TrimSpace(reply.Verdict)) {
	case verdictAllow:
		if len(reply.Warnings) > 0 {
			return verdictFix
		}
		return verdictAllow
	case verdictFix:
		return verdictFix
	case verdictBlock:
		return verdictBlock
	default:
		// Không đọc được kết luận: coi như cần người xem, không phải đạt.
		return verdictFix
	}
}

func complianceResultToMap(reply *complianceReply) map[string]any {
	blockers := make([]map[string]any, 0, len(reply.Blockers))
	for _, item := range reply.Blockers {
		if strings.TrimSpace(item.Code) == "" {
			continue
		}
		blockers = append(blockers, map[string]any{
			"ma": strings.ToUpper(strings.TrimSpace(item.Code)),
			"trich_doan": strings.TrimSpace(item.Excerpt), "sua_the_nao": strings.TrimSpace(item.Fix),
		})
	}
	warnings := make([]map[string]any, 0, len(reply.Warnings))
	for _, item := range reply.Warnings {
		if strings.TrimSpace(item.Code) == "" {
			continue
		}
		warnings = append(warnings, map[string]any{
			"ma": strings.ToUpper(strings.TrimSpace(item.Code)), "noi_dung": strings.TrimSpace(item.Content),
		})
	}
	return map[string]any{
		"ket_luan": normalizeVerdict(reply),
		"loi_chan": blockers,
		"canh_bao": warnings,
		"lich_ra_soat_de_xuat": map[string]any{
			"ngay": strings.TrimSpace(reply.Review.Date), "ly_do": strings.TrimSpace(reply.Review.Reason),
		},
	}
}

// runComplianceOverlay soát một bài. Lỗi hạ tầng trả CAN_SUA kèm lý do, không
// bao giờ trả DUOC_DANG — không soát được thì không phải là đạt.
func runComplianceOverlay(
	ctx context.Context,
	loop agent.Agent,
	sessionKey string,
	userID string,
	profile *pageProfile,
	post map[string]any,
) map[string]any {
	runID := uuid.NewString()
	runCtx, cancel := context.WithTimeout(ctx, complianceTimeout)
	defer cancel()

	result, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:    sessionKey + ":compliance:" + runID,
		Message:       buildCompliancePrompt(profile, post),
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         runID,
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     []string{complianceNoTools},
		MaxIterations: 1,
		SkillFilter:   []string{},
		LightContext:  true,
		HistoryLimit:  1,
		TraceName:     "tekshot compliance overlay",
		TraceTags:     []string{"tekshot", "compliance"},
	})
	if err != nil || result == nil {
		reason := "agent returned no result"
		if err != nil {
			reason = err.Error()
		}
		slog.Warn("tekshot: compliance overlay failed, marking the post for human review",
			"session", sessionKey, "reason", reason)
		return map[string]any{
			"ket_luan": verdictFix, "loi_chan": []map[string]any{}, "canh_bao": []map[string]any{},
			"ly_do_that_bai": reason,
		}
	}

	parsed, parseErr := parseComplianceReply(result.Content)
	if parseErr != nil {
		slog.Warn("tekshot: compliance overlay reply unreadable",
			"session", sessionKey, "reason", parseErr.Error())
		return map[string]any{
			"ket_luan": verdictFix, "loi_chan": []map[string]any{}, "canh_bao": []map[string]any{},
			"ly_do_that_bai": parseErr.Error(),
		}
	}
	return complianceResultToMap(parsed)
}

func parseComplianceReply(reply string) (*complianceReply, error) {
	object, err := extractJSONObject(reply)
	if err != nil {
		return nil, err
	}
	var parsed complianceReply
	if err := decodeJSONInto(object, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// applyComplianceOverlay soát từng bài trong batch và gắn kết quả vào chính
// bài đó.
//
// Bỏ qua khi trang chưa bật luật, khi bài đã treo exception (chưa có gì để
// soát), và khi bài thuần thông tin — bài THONG_TIN không mang nghĩa vụ quảng
// cáo nên overlay không có việc gì làm.
func applyComplianceOverlay(
	ctx context.Context,
	loop agent.Agent,
	sessionKey string,
	userID string,
	profile *pageProfile,
	batch map[string]any,
) {
	if profile == nil || batch == nil {
		return
	}
	posts, ok := batch["posts"].([]map[string]any)
	if !ok {
		return
	}
	// Trang chỉ được phép đăng bài thông tin thì không bài nào cần overlay.
	informationOnly := len(profile.Purposes) == 1 && profile.Purposes[0] == "THONG_TIN"
	for _, post := range posts {
		if exception, hasException := post["exception"].(map[string]any); hasException && len(exception) > 0 {
			continue
		}
		if informationOnly {
			continue
		}
		post["compliance"] = runComplianceOverlay(ctx, loop, sessionKey, userID, profile, post)
	}
}
