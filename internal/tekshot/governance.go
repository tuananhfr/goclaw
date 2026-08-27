package tekshot

import (
	"fmt"
	"strings"
)

// Prompt A — lớp tuân thủ của Kim chỉ nam LÕI v3.0.
//
// Tách khỏi draft_posts_tool.go vì nó BẬT THEO PAGE: page chưa có
// PAGE_PROFILE thì toàn bộ file này im lặng và bài viết ra y như trước. Đó là
// cái giữ cho page đang chạy không vỡ khi luật mới lên.
const (
	riskLow    = "LOW"
	riskMedium = "MEDIUM"
	riskHigh   = "HIGH"
)

// pageProfile là phần PAGE_PROFILE mà lượt viết cần đọc. Drupal gửi nguyên
// khối JSON; ở đây chỉ lấy những trường ràng buộc được câu chữ.
type pageProfile struct {
	Codes         []string
	Purposes      []string
	Formats       []string
	Forbidden     []string
	Required      []string
	ForbiddenTops []string
	BlockCTAPhone bool
	LegalEntity   string
}

func pageProfileFromRequest(request map[string]any) *pageProfile {
	raw, ok := request["page_profile"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	profile := &pageProfile{
		Codes:         stringSliceFromAny(raw["profile"]),
		Purposes:      stringSliceFromAny(raw["muc_dich_cho_phep"]),
		Formats:       stringSliceFromAny(raw["dinh_dang_cho_phep"]),
		Forbidden:     stringSliceFromAny(raw["cam_rieng"]),
		Required:      stringSliceFromAny(raw["bat_buoc_rieng"]),
		ForbiddenTops: stringSliceFromAny(raw["chu_de_bi_cam"]),
		BlockCTAPhone: boolFromMap(raw, "cam_cta_thu_sdt"),
		LegalEntity:   stringFromMap(raw, "phap_nhan"),
	}
	if len(profile.Codes) == 0 {
		return nil
	}
	return profile
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, isString := item.(string); isString && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// buildGovernancePrompt là Phần I + Phần II của Prompt A.
//
// Phần I là luật chung không phụ thuộc profile; Phần II đọc thẳng cam_rieng
// và bat_buoc_rieng — chép đủ, không tóm tắt, vì Lớp 1 chỉ được SIẾT.
func buildGovernancePrompt(profile *pageProfile) string {
	if profile == nil {
		return ""
	}
	var sb strings.Builder

	sb.WriteString("\n=== LUẬT NỘI DUNG BẮT BUỘC (Kim chỉ nam LÕI v3.0) ===\n")
	sb.WriteString("Bạn KHÔNG phải nhà báo. Trang này KHÔNG phải cơ quan báo chí và KHÔNG phải trang thông tin điện tử tổng hợp.\n\n")

	sb.WriteString("## PHẦN I — RÀNG BUỘC TUYỆT ĐỐI\n")
	sb.WriteString("1. NGUỒN. Chỉ dùng dữ kiện có trong nguồn được cấp. KHÔNG dùng kiến thức nền về địa phương, sản phẩm, doanh nghiệp, quy định pháp luật, giá cả. ĐẶC BIỆT: tên đơn vị hành chính Việt Nam trong kiến thức nền của bạn gần như chắc chắn đã lỗi thời sau sắp xếp 01/7/2025. Nội dung cũ trên website nhà là nguồn Cấp 3, KHÔNG phải Cấp 1.\n")
	sb.WriteString("2. DỪNG KHI THIẾU. Thiếu nguồn cho một dữ kiện → xuất exception, KHÔNG suy đoán, KHÔNG làm tròn, KHÔNG viết mơ hồ để né.\n")
	sb.WriteString("3. KHÔNG BÁO HÓA. Cấm phóng sự, điều tra, phỏng vấn, tổng hợp tin. Cấm các cụm: \"theo ghi nhận của chúng tôi\", \"phóng viên\", \"trao đổi với chúng tôi\", \"nguồn tin riêng\", \"người dân bức xúc\", \"bản tin\", \"tin nóng\", \"độc quyền\", \"khẩn\".\n")
	sb.WriteString("4. KHÔNG XẾP HẠNG BÊN NGOÀI. Cấm \"top N\", \"điểm danh\", \"N thương hiệu tốt nhất\" khi nêu tên cơ sở kinh doanh ngoài hệ sinh thái. Được phép: so sánh giữa các lựa chọn TRONG NHÀ.\n")
	sb.WriteString("5. KHÔNG LÃNG MẠN HÓA: hiền hòa, chất phác, mộc mạc, nguyên sơ, bình dị, nếp sống xưa, chưa bị đô thị hóa.\n")
	sb.WriteString("6. KHÔNG GÁN Ý NGHĨA cho hoa văn, màu sắc, kiến trúc, nghi lễ nếu nguồn không nêu rõ.\n")
	sb.WriteString("7. KHÔNG DỮ LIỆU CÁ NHÂN: tên, mô tả, ảnh, SĐT, địa chỉ của khách hàng, nhân viên, ứng viên, người dân, chủ đại lý. Ngoại lệ duy nhất: người đại diện pháp nhân trong vai trò đã công bố công khai.\n")
	sb.WriteString("8. KHÔNG BÊN THỨ BA THIẾU GIẤY. Không nêu tên, logo, nhãn hiệu, hình ảnh cơ sở của khách hàng, đối tác, nhà cung cấp, nhà sản xuất khi nguồn không kèm số văn bản đồng ý.\n")
	sb.WriteString("9. KHÔNG LỜI CHỨNG BỊA. Không gán phát ngôn, đánh giá, trải nghiệm cho người có thật khi không có văn bản xác nhận đúng câu chữ.\n")
	sb.WriteString("10. TÊN HÀNH CHÍNH. Nhắc tên cũ thì viết \"[tên mới] (trước đây thuộc [tên cũ])\", không viết ngược.\n")
	sb.WriteString("11. KHÔNG CÔNG DỤNG SỨC KHỎE cho thực phẩm: chữa bệnh, phòng bệnh, hỗ trợ điều trị, GIÁ TRỊ DINH DƯỠNG, MỨC ĐỘ PHÙ HỢP VỚI NHÓM ĐỐI TƯỢNG (trẻ nhỏ, người già, phụ nữ mang thai).\n")
	sb.WriteString("12. KHÔNG TỪ TUYỆT ĐỐI: nhất, số một, duy nhất, tốt nhất, hàng đầu, đầu tiên — trừ khi nguồn kèm tài liệu chứng minh.\n")
	sb.WriteString("13. KHÔNG CON SỐ ĐẦU TƯ THIẾU CƠ SỞ: mức tiết kiệm, thời gian hoàn vốn, doanh thu, lợi nhuận dự kiến. Nêu số thì phải ghi điều kiện tính ngay tại chỗ.\n")
	sb.WriteString("14. KHÔNG XUI KHÁCH VI PHẠM. Không mô tả tính năng theo cách hứa một kết quả mà khách chỉ đạt được nếu bỏ qua nghĩa vụ pháp lý của họ — đặc biệt tính năng thu thập dữ liệu sinh trắc học. Mô tả năng lực kỹ thuật phải kèm điều kiện triển khai.\n")
	sb.WriteString("15. KHÔNG BIỂU TƯỢNG NHÀ NƯỚC ở bất kỳ đâu, kể cả làm nền.\n\n")

	sb.WriteString("## PHẦN II — RÀNG BUỘC RIÊNG CỦA TRANG NÀY\n")
	sb.WriteString(fmt.Sprintf("Profile: %s. Pháp nhân vận hành: %s.\n",
		strings.Join(profile.Codes, " + "), orDash(profile.LegalEntity)))
	sb.WriteString(fmt.Sprintf("Mục đích được phép: %s.\n", strings.Join(profile.Purposes, ", ")))
	sb.WriteString(fmt.Sprintf("Định dạng được phép: %s.\n", strings.Join(profile.Formats, ", ")))

	if len(profile.Forbidden) > 0 {
		sb.WriteString("\nCẤM RIÊNG — áp dụng ĐẦY ĐỦ, không diễn giải lỏng đi:\n")
		for _, rule := range profile.Forbidden {
			sb.WriteString("- " + rule + "\n")
		}
	}
	if len(profile.Required) > 0 {
		sb.WriteString("\nBẮT BUỘC RIÊNG:\n")
		for _, rule := range profile.Required {
			sb.WriteString("- " + rule + "\n")
		}
	}
	if len(profile.ForbiddenTops) > 0 {
		sb.WriteString("\nCHỦ ĐỀ BỊ CẤM HOÀN TOÀN (chưa đủ hồ sơ pháp lý) — chạm vào là exception, kể cả bài thông tin:\n")
		for _, topic := range profile.ForbiddenTops {
			sb.WriteString("- " + topic + "\n")
		}
	}
	if profile.BlockCTAPhone {
		sb.WriteString("\nCẤM mọi CTA thu thập số điện thoại: chính sách bảo vệ dữ liệu cá nhân của trang chưa đạt.\n")
	}

	sb.WriteString("\n## TỰ CHẤM RỦI RO — bắt buộc kèm mỗi bài\n")
	sb.WriteString("- LOW: bài kiến thức, cẩm nang, thông báo vận hành; không con số độc quyền, không tên người, không lời hứa.\n")
	sb.WriteString("- MEDIUM: bài quảng bá sản phẩm nhà, có giá hoặc khuyến mại, không tên khách hàng.\n")
	sb.WriteString("- HIGH: có tên khách hàng, số liệu kết quả, con số đầu tư, nội dung sinh trắc học, số liệu kỹ thuật có hậu quả an toàn, hoặc chủ đề văn hoá/di sản.\n")
	sb.WriteString("Bạn PHẢI viết ly_do_cham_risk. Bạn đang tự chấm rủi ro cho bài do chính bạn viết — điểm mù của bạn nằm ở đúng chỗ bạn tự tin nhất. Viết lý do ra để người duyệt kiểm tra được suy luận.\n")

	sb.WriteString("\n## KHI CHẠM LẰN RANH\n")
	sb.WriteString("Điền `exception` thay vì cố viết cho xong: ghi rõ chạm luật nào, TRÍCH NGUYÊN VĂN đoạn gây ra, và đề xuất TỐI THIỂU HAI phương án thay thế. KHÔNG tự chọn phương án. KHÔNG tự gỡ cờ. Khi có exception thì để `content` rỗng.\n")

	return sb.String()
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// governancePostProperties là các trường Prompt A thêm vào mỗi bài.
//
// Chỉ gắn khi page có PAGE_PROFILE: schema cũ giữ nguyên cho page chưa bật.
func governancePostProperties() map[string]any {
	return map[string]any{
		"tu_cham_risk": map[string]any{
			"type":        "string",
			"description": "LOW | MEDIUM | HIGH. Tự chấm rủi ro cho bài này theo thang đã nêu trong luật.",
		},
		"ly_do_cham_risk": map[string]any{
			"type":        "string",
			"description": "BẮT BUỘC. Vì sao bài này ở mức đó — nêu dữ kiện cụ thể trong bài dẫn tới mức chấm, không viết chung chung. Người duyệt đọc lý do này để bắt điểm mù của bạn.",
		},
		"khoi_4_gioi_han": map[string]any{
			"type":        "string",
			"description": "BẮT BUỘC, 1-2 câu: điều gì chưa xác minh, dữ liệu kỳ nào, có thể thay đổi theo mùa/chi nhánh/lô hàng. Bài thương mại ghi thời hạn và điều kiện áp dụng ở đây. Bài kỹ thuật ghi điều kiện áp dụng của số liệu.",
		},
		"khoi_7_tuan_thu": map[string]any{
			"type":        "string",
			"description": "Nhãn tuân thủ đặt ở DÒNG ĐẦU bài, trong 125 ký tự đầu: 'Nội dung tài trợ' (bài trả tiền), 'Quảng cáo' (quảng bá sản phẩm nhà), 'Nội dung có liên kết tiếp thị'. Để rỗng khi bài thuần thông tin.",
		},
		"nguon_da_dung": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Mã nguồn hoặc tên tài liệu đã dùng cho các dữ kiện trong bài. Rỗng khi bài không nêu dữ kiện nào cần nguồn.",
		},
		"claim_khong_co_nguon": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Những khẳng định bạn đã viết mà KHÔNG có nguồn trỏ về. Trung thực ở đây quan trọng hơn danh sách rỗng — người duyệt cần biết chỗ nào cần kiểm.",
		},
		"media_brief": map[string]any{
			"type":        "string",
			"description": "CHỈ điền khi loai_anh là REAL hoặc PRODUCT: brief chụp cho người đi thực địa — chủ thể cần chụp, bối cảnh, số ảnh tối thiểu, những gì phải tránh. Bạn KHÔNG được sinh ảnh cho hai nhánh này.",
		},
		"lich_ra_soat": map[string]any{
			"type":        "string",
			"description": "Ngày phải rà lại bài, dạng YYYY-MM-DD, khi bài có thứ hết hạn: khuyến mại, giá, uỷ quyền nhãn hiệu, hạn xoá dữ liệu ứng viên. Rỗng khi không có gì hết hạn.",
		},
		"exception": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"description":          "Điền khi bài chạm lằn ranh và KHÔNG viết được. Có exception thì content để rỗng.",
			"properties": map[string]any{
				"lan_ranh":   map[string]any{"type": "string", "description": "Mã hoặc tên lằn ranh bị chạm, ví dụ R12 hoặc 'cấm riêng: ảnh AI của món đang bán'."},
				"trich_doan": map[string]any{"type": "string", "description": "Nguyên văn đoạn nguồn gây ra lằn ranh, không diễn giải lại."},
				"phuong_an": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "TỐI THIỂU HAI phương án thay thế. Không tự chọn — người quyết.",
				},
			},
			"required": []string{"lan_ranh", "trich_doan", "phuong_an"},
		},
	}
}

// governanceRequiredFields là những trường không được để trống khi luật bật.
func governanceRequiredFields() []string {
	return []string{"tu_cham_risk", "ly_do_cham_risk", "khoi_4_gioi_han"}
}

// normalizeRisk ép về ba mức. Giá trị lạ đọc thành HIGH, không phải LOW: đoán
// sai về phía an toàn thì chỉ tốn một lượt duyệt tay, đoán sai phía kia thì
// một bài chưa ai đọc tự lên trang.
func normalizeRisk(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case riskLow:
		return riskLow
	case riskMedium:
		return riskMedium
	default:
		return riskHigh
	}
}

// validateGovernanceFields kẹp lại phần Go phải tự bảo đảm.
//
// Schema đã ràng HÌNH DẠNG; đây là CHÍNH SÁCH: lý do chấm không được rỗng,
// exception phải có đủ hai phương án.
func validateGovernanceFields(post map[string]any, index int) error {
	if exception, ok := post["exception"].(map[string]any); ok && len(exception) > 0 {
		options := stringSliceFromAny(exception["phuong_an"])
		if len(options) < 2 {
			return fmt.Errorf("posts[%d].exception phải đề xuất tối thiểu hai phương án thay thế", index)
		}
		if strings.TrimSpace(stringFromMap(exception, "trich_doan")) == "" {
			return fmt.Errorf("posts[%d].exception phải trích nguyên văn đoạn gây ra lằn ranh", index)
		}
		// Bài có exception thì không cần lý do chấm: nó chưa được viết.
		post["tu_cham_risk"] = normalizeRisk(stringFromMap(post, "tu_cham_risk"))
		return nil
	}

	reason := strings.TrimSpace(stringFromMap(post, "ly_do_cham_risk"))
	if reason == "" {
		return fmt.Errorf("posts[%d].ly_do_cham_risk là bắt buộc — không có lý do thì điểm chấm không kiểm được", index)
	}
	post["tu_cham_risk"] = normalizeRisk(stringFromMap(post, "tu_cham_risk"))
	if strings.TrimSpace(stringFromMap(post, "khoi_4_gioi_han")) == "" {
		return fmt.Errorf("posts[%d].khoi_4_gioi_han là bắt buộc", index)
	}
	return nil
}
