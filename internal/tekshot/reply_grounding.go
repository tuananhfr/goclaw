package tekshot

import (
	"strings"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Trích dẫn ngắn hơn mức này trùng tình cờ với bất kỳ tài liệu nào đã đọc, nên không tính là bằng chứng.
const replyQuoteMinRunes = 12

// social = chỉ xã giao, không mang thông tin; các loại còn lại phải chỉ ra được nguồn.
// Drupal ReplyComposeResult::GROUNDINGS giữ cùng danh sách.
var replyGroundings = map[string]bool{"social": true, "sourced": true, "profile": true, "post": true}

const replyGroundingRules = `
## Nguồn thông tin — bạn là nhân viên của Page, chỉ biết những gì Page cung cấp
- Nguồn DUY NHẤT: dữ liệu đã duyệt, kịch bản của quản lý, hồ sơ Page, bài đăng (nếu có) và đoạn tài liệu bạn đã đọc bằng vault_read. KHÔNG dùng hiểu biết riêng, KHÔNG suy đoán, KHÔNG lấy thông tin trên mạng — kể cả điều nghe có vẻ hiển nhiên.
- Không cần nguồn: chào hỏi, cảm ơn, xác nhận đã nhận tin, hỏi lại cho rõ, xin thông tin để hỗ trợ. Câu loại này grounding="social" và KHÔNG được chứa thông tin nào về sản phẩm, dịch vụ, giá, thời gian, địa chỉ, chính sách, cách dùng hay nhận xét chất lượng.
- Có thông tin cụ thể thì phải khai nguồn: grounding="sourced" (kèm row_ids, script_index hoặc quotes chép nguyên văn từ vault_read), "profile" (lấy từ hồ sơ Page) hoặc "post" (lấy từ bài đăng).
- Khách hỏi điều nguồn không có → không trả lời phần đó, action "hold". hold_text chỉ là câu giữ chân chung chung, không kèm thông tin hay lời hứa.
`

// applyReplyGrounding chép grounding của model và hạ reply thành hold khi câu AI viết không khai được nguồn thật.
// Kịch bản nguyên văn do quản lý viết nên tự nó là nguồn.
func applyReplyGrounding(result, report, request map[string]any, scripts []messengerComposeScript) {
	grounding, _ := report["grounding"].(string)
	if !replyGroundings[grounding] {
		grounding = ""
	}
	result["grounding"] = grounding
	if result["action"] != "reply" || messengerComposeUsesVerbatim(result, scripts) {
		return
	}
	if !replyGrounded(grounding, result, request) {
		result["action"], result["reason"] = "hold", "ungrounded"
	}
}

func replyGrounded(grounding string, result, request map[string]any) bool {
	switch grounding {
	case "social":
		return true
	case "sourced":
		rows, _ := result["row_ids"].([]string)
		quotes, _ := result["quotes"].([]string)
		index, _ := result["script_index"].(int)
		return len(rows) > 0 || len(quotes) > 0 || index > 0
	case "profile":
		return strings.TrimSpace(stringFromMap(request, "profile")) != ""
	case "post":
		post, _ := request["post"].(map[string]any)
		return strings.TrimSpace(stringFromMap(post, "title")+stringFromMap(post, "content")) != ""
	}
	return false
}

// enforceVaultEvidence: mỗi trích dẫn phải nằm trong nội dung vault_read thật sự trả về, không thì không gửi.
func enforceVaultEvidence(result map[string]any, evidence *tools.VaultReadEvidence) {
	quotes, _ := result["quotes"].([]string)
	for _, quote := range quotes {
		if utf8.RuneCountInString(strings.TrimSpace(quote)) < replyQuoteMinRunes || !evidence.Contains(quote) {
			if result["action"] == "reply" {
				result["action"] = "hold"
			}
			result["reason"] = "unverified_vault_quote"
			result["quotes"] = []string{}
			return
		}
	}
}
