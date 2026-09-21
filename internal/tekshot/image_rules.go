package tekshot

import "strings"

// Prompt B — luật ảnh AI.
//
// Chỉ áp cho đường TỰ ĐỘNG (runAutoImage): cron sinh ảnh không ai xem nên phải
// fail-closed. Chat tay là người cố ý bấm và tự chịu — runChat không gắn luật
// này cho image_chat (quyết định 2026-09-07). Nhãn thì làm việc dùng ảnh AI
// HỢP PHÁP, không làm nó ĐÚNG:
// một chiếc pizza AI vẽ, gắn nhãn đầy đủ, vẫn khiến khách nhận hàng thấy khác
// ảnh — và với ảnh sản phẩm đó là quảng cáo gây nhầm lẫn, không chỉ mất uy tín.
const imageRulesBlock = `
=== LUẬT ẢNH BẮT BUỘC (NĐ 142/2026, Kim chỉ nam LÕI v3.0 mục 4) ===

TUYỆT ĐỐI KHÔNG sinh ảnh mô phỏng:
- Món ăn, đồ uống đang bán; bao bì thật; nhãn hàng hoá thật
- Sản phẩm vật lý thật (vật liệu, thiết bị, hàng hoá đang phân phối)
- Giao diện phần mềm, dashboard, biểu đồ số liệu
- Chân dung người, ảnh đội ngũ, ảnh khách hàng, ảnh công nhân
- Địa điểm có thật, di tích, kiến trúc bản địa, công trình đã thi công
- Trang phục dân tộc, nhạc cụ truyền thống, lễ hội, hoa văn dân tộc có thật
- Văn phòng, nhà máy, cửa hàng thật
- Logo, nhãn hiệu bất kỳ; biểu tượng, con dấu, phù hiệu cơ quan nhà nước
- Bất kỳ hình nào mà người biết rõ chuyện có thể nhìn vào và nói "chỗ này không đúng"

ĐƯỢC PHÉP: nền trừu tượng; hoa văn hình học không mô phỏng hoa văn dân tộc có
thật; gradient màu; thẻ chữ; biểu tượng dạng icon; sơ đồ khái niệm không mô
phỏng vật thể thật.

Prompt định dùng có chứa yếu tố cấm → DỪNG, không sinh ảnh. Nói rõ vì sao và
đề xuất chụp ảnh thật hoặc dựng infographic thay thế.

INFOGRAPHIC: chỉ đưa lên hình những con số có nguồn kèm theo. Không thêm số
nào khác. Ghi nguồn và kỳ dữ liệu ngay trên hình, góc dưới KHÔNG trùng vùng logo
(BRAND MARK AREA), cỡ chữ đọc được trên điện thoại. Không bản đồ hành chính tự vẽ.

ẢNH THẬT: được chỉnh sáng, tương phản, cân bằng trắng, khử nhiễu, cắt cúp —
đó là chỉnh kỹ thuật, không đổi bản chất. KHÔNG được thêm/bớt nguyên liệu,
đổi màu, ghép nền, làm đầy đặn hơn thực tế, xoá khuyết điểm cấu trúc, thêm
hạng mục chưa thi công. Với ảnh sản phẩm, quy tắc nội bộ là KHÔNG chỉnh bằng
AI, kể cả khi có nhãn.
`

// defaultMediaBranchRules là ràng buộc riêng theo nhánh của bài. Không có nhánh
// AI: lớp cấm ở trên đã đủ.
var defaultMediaBranchRules = map[string]string{
	"UPLOAD": "BÀI NÀY CẦN ẢNH THẬT. KHÔNG sinh ảnh. Xuất brief chụp cho người đi thực địa: chủ thể cần chụp (cụ thể, không mô tả cảm xúc), bối cảnh, khung giờ ánh sáng, số ảnh tối thiểu, và những gì phải tránh.",
	"REF":    "BÀI NÀY DỰNG TỪ ẢNH THẬT TRONG KHO. Ảnh kho được chọn sẵn là chỗ dựa về chủ thể, chất liệu và không khí — bám sát nó thay vì tưởng tượng lại từ đầu.",
	"INFO":   "BÀI NÀY DÙNG INFOGRAPHIC. Tỷ lệ 4:5 dọc, tối đa 7 điểm dữ liệu một hình. Nhãn bắt buộc trên hình: \"Đồ họa tạo bằng AI. Nguồn số liệu: [tên nguồn].\"",
}

// mediaBranchKey gộp từ vựng cũ: UPLOAD/REF/AI là hiện tại; REAL/PRODUCT/INFO
// là của bài viết trước đợt tách trục và vẫn phải đọc được.
func mediaBranchKey(branch string) string {
	switch strings.ToUpper(strings.TrimSpace(branch)) {
	case "UPLOAD", "REAL", "PRODUCT":
		return "UPLOAD"
	case "REF":
		return "REF"
	case "INFO":
		return "INFO"
	default:
		return ""
	}
}

func mediaBranchRules(branch string) string {
	return mediaBranchRulesFrom(defaultMediaBranchRules, branch)
}

func mediaBranchRulesFrom(rules map[string]string, branch string) string {
	text := rules[mediaBranchKey(branch)]
	if text == "" {
		return ""
	}
	return "\n" + text + "\n"
}

// imageGuidanceFor dựng khối luật cho một lượt sinh ảnh tự động.
//
// Trả rỗng khi trang chưa bật luật: lượt sinh ảnh giữ nguyên hành vi cũ, đúng
// nguyên tắc bật-theo-page của cả đợt này. Chỉ automatedImagePrompt gọi nó.
func imageGuidanceFor(request map[string]any) string {
	profile := pageProfileFromRequest(request)
	if profile == nil {
		return ""
	}
	return "\n" + profile.Rules.ImageRules + "\n" +
		mediaBranchRulesFrom(profile.Rules.MediaBranchRules, stringFromMap(request, "loai_anh"))
}

// aiImageLabel là nhãn phải nằm ở DÒNG ĐẦU caption. Drupal chèn bằng code chứ
// không tin prompt: nhãn là nghĩa vụ pháp lý, không phải một lời nhắc.
const aiImageLabel = "Hình minh họa được tạo bằng AI."
