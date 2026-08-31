package tekshot

import "strings"

// Prompt B — luật ảnh AI.
//
// Đây là LỚP XUYÊN SUỐT, không phải luật của một nhánh ảnh. Nó cắm ở runner
// image_chat nên phủ cả auto_image (auto_image bọc runChat) và mọi đường sinh
// ảnh thêm sau này. Nhãn thì làm việc dùng ảnh AI HỢP PHÁP, không làm nó ĐÚNG:
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
nào khác. Ghi nguồn và kỳ dữ liệu ngay trên hình, góc dưới, cỡ chữ đọc được
trên điện thoại. Không bản đồ hành chính tự vẽ.

ẢNH THẬT: được chỉnh sáng, tương phản, cân bằng trắng, khử nhiễu, cắt cúp —
đó là chỉnh kỹ thuật, không đổi bản chất. KHÔNG được thêm/bớt nguyên liệu,
đổi màu, ghép nền, làm đầy đặn hơn thực tế, xoá khuyết điểm cấu trúc, thêm
hạng mục chưa thi công. Với ảnh sản phẩm, quy tắc nội bộ là KHÔNG chỉnh bằng
AI, kể cả khi có nhãn.
`

// mediaBranchRules là ràng buộc riêng theo nhánh của bài. Rỗng cho nhánh AI:
// lớp cấm ở trên đã đủ.
func mediaBranchRules(branch string) string {
	// UPLOAD/REF/AI là từ vựng hiện tại; REAL/PRODUCT/INFO là của bài viết
	// trước đợt tách trục và vẫn phải đọc được.
	switch strings.ToUpper(strings.TrimSpace(branch)) {
	case "UPLOAD", "REAL", "PRODUCT":
		return "\nBÀI NÀY CẦN ẢNH THẬT. KHÔNG sinh ảnh. Xuất brief chụp cho người đi thực địa: chủ thể cần chụp (cụ thể, không mô tả cảm xúc), bối cảnh, khung giờ ánh sáng, số ảnh tối thiểu, và những gì phải tránh.\n"
	case "REF":
		return "\nBÀI NÀY DỰNG TỪ ẢNH THẬT TRONG KHO. Ảnh kho được chọn sẵn là chỗ dựa về chủ thể, chất liệu và không khí — bám sát nó thay vì tưởng tượng lại từ đầu.\n"
	case "INFO":
		return "\nBÀI NÀY DÙNG INFOGRAPHIC. Tỷ lệ 4:5 dọc, tối đa 7 điểm dữ liệu một hình. Nhãn bắt buộc trên hình: \"Đồ họa tạo bằng AI. Nguồn số liệu: [tên nguồn].\"\n"
	default:
		return ""
	}
}

// imageGuidanceFor dựng khối luật cho một lượt sinh ảnh.
//
// Trả rỗng khi trang chưa bật luật: lượt sinh ảnh giữ nguyên hành vi cũ, đúng
// nguyên tắc bật-theo-page của cả đợt này.
func imageGuidanceFor(request map[string]any) string {
	if pageProfileFromRequest(request) == nil {
		return ""
	}
	return imageRulesBlock + mediaBranchRules(stringFromMap(request, "loai_anh"))
}

// aiImageLabel là nhãn phải nằm ở DÒNG ĐẦU caption. Drupal chèn bằng code chứ
// không tin prompt: nhãn là nghĩa vụ pháp lý, không phải một lời nhắc.
const aiImageLabel = "Hình minh họa được tạo bằng AI."
