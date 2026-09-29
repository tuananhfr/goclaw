package tekshot

import (
	"fmt"
	"sort"
	"strings"
)

// Các cột kế hoạch của Content Master: một dòng checklist nói bài viết cho ai,
// ở giai đoạn nào, kêu gọi gì. Drupal lưu mã (không dấu), giao diện dịch ra
// nhãn — đổi một mã ở đây là phải đổi InsightChecklistPlanFields.php cùng lúc.
var (
	checklistStages = []string{"NHAN_BIET", "CAN_NHAC", "HANH_DONG"}

	checklistCTAs = []string{
		"THEO_DOI", "LUU_BAI", "CHIA_SE", "BINH_LUAN",
		"COMMENT_TU_KHOA", "INBOX", "ZALO", "DANG_KY",
		"HOTLINE", "DAT_HANG",
	}

	checklistEmotions = []string{
		"TU_HAO", "AN_TAM", "TO_MO", "CAM_HUNG", "VUI", "AM_AP", "THANH_TUU", "DUOC_THAU_HIEU",
	}

	checklistImageSources = []string{"UPLOAD", "REF", "AI"}
	checklistImageStyles  = []string{"PHOTOREAL", "POSTER", "INFOGRAPHIC", "QUOTE", "CONCEPT"}
	checklistRisks        = []string{riskLow, riskMedium, riskHigh}
	checklistSourceLevels = []string{"CAP_1", "CAP_2", "CAP_3"}
	checklistAllPurposes  = []string{"THONG_TIN", "THUONG_MAI", "HOP_TAC", "TUYEN_DUNG"}
)

// CTA mà giai đoạn phễu cho phép: bài nhận biết chỉ trò chuyện, cân nhắc mời
// tìm hiểu, hành động mới được mời mua.
var checklistStageCTAs = map[string][]string{
	"NHAN_BIET": {"THEO_DOI", "LUU_BAI", "CHIA_SE", "BINH_LUAN"},
	"CAN_NHAC":  {"THEO_DOI", "LUU_BAI", "CHIA_SE", "BINH_LUAN", "COMMENT_TU_KHOA", "INBOX", "ZALO", "DANG_KY"},
	"HANH_DONG": checklistCTAs,
}

// CTA bán hàng không đi với bài thông tin.
var checklistSalesCTAs = []string{"HOTLINE", "DAT_HANG"}

const checklistCTAKeywordMaxRunes = 30

// checklistPlanFrame là khung Drupal gửi kèm request: định dạng/mục đích trang
// được phép và các nhóm khách trong hồ sơ doanh nghiệp.
type checklistPlanFrame struct {
	Formats   map[string]string
	Purposes  []string
	Audiences []string
}

func checklistPlanFrameFromRequest(request map[string]any) checklistPlanFrame {
	frame := checklistPlanFrame{Formats: map[string]string{}}
	raw, _ := request["plan_frame"].(map[string]any)
	if formats, ok := raw["formats"].(map[string]any); ok {
		for code, label := range formats {
			code = strings.ToUpper(strings.TrimSpace(code))
			if code != "" {
				frame.Formats[code] = strings.TrimSpace(fmt.Sprint(label))
			}
		}
	}
	for _, purpose := range stringSliceFromAny(raw["purposes"]) {
		purpose = strings.ToUpper(purpose)
		if containsString(checklistAllPurposes, purpose) {
			frame.Purposes = append(frame.Purposes, purpose)
		}
	}
	if len(frame.Purposes) == 0 {
		frame.Purposes = checklistAllPurposes
	}
	frame.Audiences = stringSliceFromAny(raw["audiences"])
	return frame
}

func (f checklistPlanFrame) formatCodes() []string {
	codes := make([]string, 0, len(f.Formats))
	for code := range f.Formats {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return formatOrder(codes[i]) < formatOrder(codes[j]) })
	return codes
}

// formatOrder xếp F2 trước F10 thay vì theo chữ.
func formatOrder(code string) int {
	var n int
	if _, err := fmt.Sscanf(code, "F%d", &n); err != nil {
		return 1 << 20
	}
	return n
}

// checklistPlanProperties là phần schema các cột kế hoạch thêm vào mỗi dòng.
func checklistPlanProperties(frame checklistPlanFrame) map[string]any {
	enumString := func(values []string, description string) map[string]any {
		return map[string]any{"type": "string", "enum": values, "description": description}
	}
	formats := frame.formatCodes()
	formatProperty := map[string]any{"type": "string", "description": "Format code of this post, from the allowed list in the prompt."}
	if len(formats) > 0 {
		formatProperty["enum"] = formats
	}
	return map[string]any{
		"tep_khach":       map[string]any{"type": "string", "description": "The one customer group this post speaks to. When the business profile lists groups, copy one name exactly."},
		"giai_doan":       enumString(checklistStages, "Funnel stage: NHAN_BIET (awareness), CAN_NHAC (consideration), HANH_DONG (action)."),
		"cta_chinh":       enumString(checklistCTAs, "Exactly one primary call to action, allowed by the funnel stage."),
		"tu_khoa_cta":     map[string]any{"type": "string", "description": "Comment keyword, only when cta_chinh is COMMENT_TU_KHOA (short, upper case, e.g. BẢNG GIÁ). Empty otherwise."},
		"cam_xuc":         enumString(checklistEmotions, "The feeling the reader should leave with."),
		"dinh_dang":       formatProperty,
		"muc_dich":        enumString(frame.Purposes, "Purpose of the post."),
		"loai_anh":        enumString(checklistImageSources, "Image source: UPLOAD = a real photo a person takes or picks (real dishes, products, people, places); REF = generated from a real library photo; AI = fully generated."),
		"style_anh":       map[string]any{"type": "string", "description": "PHOTOREAL | POSTER | INFOGRAPHIC | QUOTE | CONCEPT when loai_anh is REF or AI. Empty for UPLOAD."},
		"rui_ro":          enumString(checklistRisks, "Planning risk: LOW = knowledge, guide, operating notice; MEDIUM = own product with price or promotion; HIGH = customer names, result figures, investment numbers, heritage or safety topics."),
		"nguon_toi_thieu": map[string]any{"type": "string", "description": "CAP_1 when the post makes a product, technical, capability or investment claim that needs an official source; CAP_2 or CAP_3 for lighter claims; empty when the post states no fact that needs a source."},
		"kieu_hook":       enumString(checklistHookTypes, "How the hook opens: CAU_HOI question, CON_SO number, CAU_CHUYEN story, NGHICH_LY paradox, MEO_NHANH quick tip, TUYEN_BO statement, SO_SANH comparison."),
		"cot_truyen":      enumString(checklistStoryTypes, "The storyline of the post: TRUOC_SAU before-after, HAU_TRUONG behind the scenes, CHUYEN_KHACH customer story, HUONG_DAN how-to, SO_SANH comparison, SAI_LAM common mistake, DIP_SU_KIEN occasion or event, GIOI_THIEU introduction."),
		"diem_chu_de":     checklistScoreProperty(true),
	}
}

func checklistPlanFieldNames() []string {
	return []string{"tep_khach", "giai_doan", "cta_chinh", "tu_khoa_cta", "cam_xuc", "dinh_dang", "muc_dich", "loai_anh", "style_anh", "rui_ro", "nguon_toi_thieu", "kieu_hook", "cot_truyen", "diem_chu_de"}
}

// validateChecklistPlanFields chuẩn hoá tại chỗ rồi kiểm luật; lỗi trả về để
// model sửa, không đoán thay nó.
func validateChecklistPlanFields(item map[string]any, frame checklistPlanFrame, index int) error {
	upper := func(key string) string {
		value := strings.ToUpper(strings.TrimSpace(stringFromMap(item, key)))
		item[key] = value
		return value
	}
	requireIn := func(key string, allowed []string) (string, error) {
		value := upper(key)
		if !containsString(allowed, value) {
			return "", fmt.Errorf("items[%d].%s must be one of %s, got %q", index, key, strings.Join(allowed, ", "), value)
		}
		return value, nil
	}

	stage, err := requireIn("giai_doan", checklistStages)
	if err != nil {
		return err
	}
	purpose, err := requireIn("muc_dich", frame.Purposes)
	if err != nil {
		return err
	}
	cta, err := requireIn("cta_chinh", checklistCTAs)
	if err != nil {
		return err
	}
	if !containsString(checklistStageCTAs[stage], cta) {
		return fmt.Errorf("items[%d].cta_chinh %s does not fit stage %s; allowed: %s", index, cta, stage, strings.Join(checklistStageCTAs[stage], ", "))
	}
	if purpose == "THONG_TIN" && containsString(checklistSalesCTAs, cta) {
		return fmt.Errorf("items[%d] is an information post (THONG_TIN) and cannot use the sales CTA %s", index, cta)
	}

	keyword := strings.ToUpper(strings.TrimSpace(stringFromMap(item, "tu_khoa_cta")))
	if cta == "COMMENT_TU_KHOA" {
		if keyword == "" {
			return fmt.Errorf("items[%d].tu_khoa_cta is required when cta_chinh is COMMENT_TU_KHOA", index)
		}
		if len([]rune(keyword)) > checklistCTAKeywordMaxRunes {
			return fmt.Errorf("items[%d].tu_khoa_cta must be at most %d characters", index, checklistCTAKeywordMaxRunes)
		}
	} else {
		keyword = ""
	}
	item["tu_khoa_cta"] = keyword

	if _, err := requireIn("cam_xuc", checklistEmotions); err != nil {
		return err
	}
	if len(frame.Formats) > 0 {
		if _, err := requireIn("dinh_dang", frame.formatCodes()); err != nil {
			return err
		}
	} else {
		upper("dinh_dang")
	}
	source, err := requireIn("loai_anh", checklistImageSources)
	if err != nil {
		return err
	}
	style := upper("style_anh")
	if source == "UPLOAD" {
		item["style_anh"] = ""
	} else if style != "" && !containsString(checklistImageStyles, style) {
		return fmt.Errorf("items[%d].style_anh must be one of %s or empty", index, strings.Join(checklistImageStyles, ", "))
	}
	// Rủi ro lạ đọc thành HIGH như lượt viết bài: đoán sai về phía an toàn.
	item["rui_ro"] = normalizeRisk(stringFromMap(item, "rui_ro"))
	if level := upper("nguon_toi_thieu"); level != "" && !containsString(checklistSourceLevels, level) {
		return fmt.Errorf("items[%d].nguon_toi_thieu must be CAP_1, CAP_2, CAP_3 or empty", index)
	}
	if _, err := requireIn("kieu_hook", checklistHookTypes); err != nil {
		return err
	}
	if _, err := requireIn("cot_truyen", checklistStoryTypes); err != nil {
		return err
	}
	score, err := normalizeChecklistScore(item["diem_chu_de"], fmt.Sprintf("items[%d]", index))
	if err != nil {
		return err
	}
	item["diem_chu_de"] = score

	audience := strings.TrimSpace(stringFromMap(item, "tep_khach"))
	if len(frame.Audiences) > 0 {
		matched := ""
		for _, name := range frame.Audiences {
			if strings.EqualFold(name, audience) {
				matched = name
				break
			}
		}
		if matched == "" {
			return fmt.Errorf("items[%d].tep_khach must be one of the business profile's customer groups: %s", index, strings.Join(frame.Audiences, " | "))
		}
		audience = matched
	} else if audience == "" {
		return fmt.Errorf("items[%d].tep_khach is required", index)
	}
	item["tep_khach"] = audience
	return nil
}

// writeChecklistPlanRules giải thích các cột kế hoạch cho người lập kế hoạch.
func writeChecklistPlanRules(sb *strings.Builder, frame checklistPlanFrame) {
	sb.WriteString("## Planning columns (Content Master)\n")
	if len(frame.Audiences) > 0 {
		sb.WriteString("- tep_khach: the one customer group this post speaks to. Copy exactly one of: " + strings.Join(frame.Audiences, " | ") + ". Spread the plan across the groups instead of writing every row for the first one.\n")
	} else {
		sb.WriteString("- tep_khach: the one customer group this post speaks to, named concretely (who they are, not 'everyone').\n")
	}
	sb.WriteString("- giai_doan: NHAN_BIET (they do not know the page yet), CAN_NHAC (they are comparing), HANH_DONG (they are ready to buy or apply). Mix the stages; a plan of only HANH_DONG rows reads as constant selling.\n")
	sb.WriteString("- cta_chinh: exactly ONE primary call to action. NHAN_BIET may only use THEO_DOI, LUU_BAI, CHIA_SE, BINH_LUAN. CAN_NHAC may add COMMENT_TU_KHOA, INBOX, ZALO, DANG_KY. HANH_DONG may also use HOTLINE, DAT_HANG. A THONG_TIN post never uses HOTLINE or DAT_HANG.\n")
	sb.WriteString("- tu_khoa_cta: only for COMMENT_TU_KHOA — the short word readers comment (e.g. BẢNG GIÁ). It must match what the page's reply scripts answer. Empty for every other CTA.\n")
	sb.WriteString("- cam_xuc: TU_HAO (pride), AN_TAM (reassured), TO_MO (curious), CAM_HUNG (inspired), VUI (fun), AM_AP (warm), THANH_TUU (accomplished), DUOC_THAU_HIEU (understood). Vary it across the plan.\n")
	if len(frame.Formats) > 0 {
		sb.WriteString("- dinh_dang: the post format. Allowed for this page:\n")
		for _, code := range frame.formatCodes() {
			sb.WriteString("  - " + code + ": " + frame.Formats[code] + "\n")
		}
	}
	sb.WriteString("- muc_dich: " + strings.Join(frame.Purposes, " | ") + ". THONG_TIN informs and never sells.\n")
	sb.WriteString("- loai_anh: UPLOAD when the image must be a real photo (real dishes, products, people, places, finished work); REF when a real library photo guides a generated image; AI only for concepts, graphics and illustrations. style_anh applies to REF and AI only.\n")
	sb.WriteString("- rui_ro: LOW for knowledge, guides and operating notices; MEDIUM for own products with a price or promotion; HIGH for customer names, result figures, investment numbers, heritage or safety topics.\n")
	sb.WriteString("- nguon_toi_thieu: CAP_1 when the post claims a product, technical, capability or investment fact; leave empty when the post states nothing that needs a source.\n")
	sb.WriteString("- kieu_hook: how the hook opens — CAU_HOI (question), CON_SO (number), CAU_CHUYEN (story), NGHICH_LY (paradox), MEO_NHANH (quick tip), TUYEN_BO (statement), SO_SANH (comparison). Never the same kieu_hook on two consecutive rows.\n")
	sb.WriteString(fmt.Sprintf("- cot_truyen: the storyline — TRUOC_SAU (before-after), HAU_TRUONG (behind the scenes), CHUYEN_KHACH (customer story), HUONG_DAN (how-to), SO_SANH (comparison), SAI_LAM (common mistake), DIP_SU_KIEN (occasion or event), GIOI_THIEU (introduction). Never the same cot_truyen within %d days.\n", checklistStoryNoRepeatDays))
	sb.WriteString(fmt.Sprintf("- Never propose a topic that overlaps an earlier one by %d%% of its word pairs or more — see the recent topics below. Rows that repeat are sent back to you to rewrite; after %d rewrites they are kept with a warning for the team.\n", int(checklistTopicOverlapMax*100), checklistMaxRepeatRewrites))
	sb.WriteString("- The body's 'Nội dung:' part must end with the one CTA named in cta_chinh, worded for that stage.\n\n")
	writeChecklistScoreRules(sb)
}
