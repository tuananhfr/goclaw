package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Prompt E — thẩm định tài liệu trước khi nạp vào Kho kiến thức.
//
// Một lỗi trên website là một lỗi; cùng lỗi đó nằm trong Kho kiến thức là lỗi
// nhân với số bài agent sẽ viết. Cổng này chạy sau extract/crawl và trước khi
// pages[] về Drupal, nên không trang nào vào vault mà chưa qua thẩm định.
const (
	// Mặc định của Prompt E là TỪ CHỐI: một trang không có phán quyết trong
	// câu trả lời bị coi là từ chối, không phải chấp nhận.
	knowledgeScreenAccept    = "CHAP_NHAN"
	knowledgeScreenAcceptFix = "CHAP_NHAN_SAU_KHI_SUA"
	knowledgeScreenReject    = "TU_CHOI"

	// Tool-free như reference_choice: ToolAllow rỗng cấp TOÀN BỘ toolset chứ
	// không phải không có tool, nên phải là một tên không khớp gì.
	knowledgeScreenNoTools = "kx-screen/no-tools"

	// Trần một lượt chấm. Website tối đa 12 trang → hai lượt; file có thể tới
	// knowledgeFileMaxChunks (400) đoạn → chia lô, không phải 400 lượt.
	knowledgeScreenBatchPages = 8
	// Thẩm định đọc phần đầu mỗi trang, không đọc hết: dấu hiệu E1–E9 nằm ở
	// tiêu đề và đoạn mở. Giới hạn này được ghi lại trong report.
	//
	// Hai hằng này quyết định ngân sách input một lượt: tối đa
	// knowledgeScreenBatchPages * knowledgeScreenExcerptChars ký tự. Nâng một
	// trong hai là nâng ngân sách đó — không có trần ký tự riêng để đỡ.
	knowledgeScreenExcerptChars = 3000
	knowledgeScreenTimeout      = 120 * time.Second
)

type knowledgeScreenExclusion struct {
	Code    string `json:"ma"`
	Excerpt string `json:"trich_doan"`
	Where   string `json:"vi_tri"`
}

type knowledgeScreenClaim struct {
	Content  string `json:"noi_dung"`
	Value    string `json:"gia_tri"`
	NeedsDoc string `json:"can_tai_lieu_gi"`
}

type knowledgeScreenConflict struct {
	Fact    string `json:"du_kien"`
	ValueA  string `json:"gia_tri_A"`
	ValueB  string `json:"gia_tri_B"`
	SourceA string `json:"nguon_A"`
	SourceB string `json:"nguon_B"`
}

type knowledgeScreenVerdict struct {
	Index      int                        `json:"index"`
	Decision   string                     `json:"quyet_dinh"`
	Exclusions []knowledgeScreenExclusion `json:"co_loai_tru"`
	Claims     []knowledgeScreenClaim     `json:"so_lieu_can_dua_vao_CLAIM_MASTER"`
	Conflicts  []knowledgeScreenConflict  `json:"mau_thuan_phat_hien"`
	UseFor     string                     `json:"dung_lam_gi"`
	Note       string                     `json:"ghi_chu_cho_nguoi_duyet"`
}

type knowledgeScreenReply struct {
	Results []knowledgeScreenVerdict `json:"ket_qua"`
}

// knowledgeScreenExcerpt cắt phần đầu một trang cho prompt thẩm định.
func knowledgeScreenExcerpt(md string) string {
	r := []rune(md)
	if len(r) <= knowledgeScreenExcerptChars {
		return md
	}
	return string(r[:knowledgeScreenExcerptChars]) + "\n…[cắt]"
}

// knowledgeScreenBatches chia pages thành các lô vừa một lượt chấm. Chỉ cắt
// theo số trang: excerpt đã chặn mỗi trang ở knowledgeScreenExcerptChars, nên
// kích thước lô bị chặn sẵn và một trần ký tự riêng sẽ không bao giờ chạm.
func knowledgeScreenBatches(pages []map[string]any) [][]int {
	batches := make([][]int, 0, (len(pages)+knowledgeScreenBatchPages-1)/knowledgeScreenBatchPages)
	for start := 0; start < len(pages); start += knowledgeScreenBatchPages {
		end := start + knowledgeScreenBatchPages
		if end > len(pages) {
			end = len(pages)
		}
		batch := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			batch = append(batch, i)
		}
		batches = append(batches, batch)
	}
	return batches
}

func buildKnowledgeScreenPrompt(pages []map[string]any, indexes []int) string {
	var sb strings.Builder
	sb.WriteString("Bạn đang thẩm định tài liệu trước khi nạp vào Kho kiến thức của agent viết bài.\n")
	sb.WriteString("Tài liệu có thể là bài viết cũ trên website, tài liệu bán hàng, hoặc tư liệu bên ngoài.\n")
	sb.WriteString("MẶC ĐỊNH: TỪ CHỐI. Chỉ chấp nhận khi tài liệu vượt qua toàn bộ kiểm tra dưới đây.\n\n")

	sb.WriteString("## KIỂM TRA LOẠI TRỪ\n")
	sb.WriteString("Đánh dấu TU_CHOI nếu tài liệu có bất kỳ dấu hiệu nào:\n")
	sb.WriteString("E1. Xếp hạng, điểm danh, so sánh, hoặc nêu tên một CƠ SỞ KINH DOANH KHÁC (quán, cửa hàng, thương hiệu đối thủ). Dạng \"top N\", \"N quán ngon nhất\", \"N thương hiệu không nên bỏ qua\"\n")
	sb.WriteString("E2. Câu KHẲNG ĐỊNH về dinh dưỡng, sức khỏe, công dụng, hoặc mức độ phù hợp với nhóm đối tượng (trẻ nhỏ, người già, bà bầu), gắn với sản phẩm đang bán\n")
	sb.WriteString("E3. Từ tuyệt đối dùng làm LỜI QUẢNG CÁO: nhất, số một, duy nhất, tốt nhất, hàng đầu, đầu tiên\n")
	sb.WriteString("E4. Con số về hiệu quả đầu tư, mức tiết kiệm, hoàn vốn, doanh thu hoặc lợi nhuận dự kiến\n")
	sb.WriteString("E5. Lời chứng, phát ngôn, đánh giá gán cho MỘT NGƯỜI CÓ THẬT; chữ mẫu (\"Text text text\", \"Lorem ipsum\"); ảnh placeholder\n")
	sb.WriteString("E6. Tên riêng của một PHÁP NHÂN hoặc THƯƠNG HIỆU BÊN THỨ BA — khách hàng, đối tác, nhà cung cấp, nhà sản xuất, nhà nhập khẩu, nhà phân phối\n")
	sb.WriteString("E7. Cấu trúc phóng sự, điều tra, phỏng vấn hỏi–đáp, hoặc tổng hợp tin từ báo chí\n")
	sb.WriteString("E8. Tên đơn vị hành chính có thể đã lỗi thời sau sắp xếp 01/7/2025 (còn ghi \"quận\", \"huyện\", \"thị trấn\" đã bỏ, hoặc tỉnh đã sáp nhập)\n")
	sb.WriteString("E9. Thông tin nhận dạng của MỘT CÁ NHÂN cụ thể: họ tên, ảnh chân dung, số điện thoại cá nhân, địa chỉ nhà, CV\n")
	sb.WriteString("E10. Số liệu mâu thuẫn với một tài liệu khác trong cùng lô đang nạp\n\n")

	// Không có khối này, model gắn cờ mọi danh từ riêng: đo thực tế trên
	// pizzahips.com cho 5/7 dương tính giả ở E6, toàn bộ là "phô mai mozzarella".
	sb.WriteString("## KHÔNG PHẢI LOẠI TRỪ — đừng gắn cờ những thứ sau\n")
	sb.WriteString("- Tên NGUYÊN LIỆU, vật liệu, giống loài, loại hàng: phô mai mozzarella, cheddar, parmesan, bột mì, thịt ba chỉ, xi măng, thép. Đây là danh từ chung, KHÔNG phải nhãn hiệu (E6).\n")
	sb.WriteString("- Liệt kê thành phần, quy cách, kích thước, khối lượng, hạn dùng, cách bảo quản. Đây là mô tả sản phẩm, KHÔNG phải khẳng định dinh dưỡng (E2).\n")
	sb.WriteString("- Tên, sản phẩm, địa chỉ, hotline của CHÍNH doanh nghiệp chủ tài liệu và các thương hiệu cùng nhà. Bên thứ ba nghĩa là NGƯỜI KHÁC (E6, E1).\n")
	sb.WriteString("- Tên riêng nằm trong tên sản phẩm hoặc tên gọi đã đặt, ví dụ \"Bột mì địa cầu 999\", \"Pizza 4 loại phô mai\" (E3, E6).\n")
	sb.WriteString("- Link hoặc mô tả ảnh SẢN PHẨM THẬT. E5 nói về ảnh PLACEHOLDER và chữ mẫu chưa xoá, không phải ảnh thật của hàng đang bán.\n")
	sb.WriteString("- Giá, khuyến mại, giờ mở cửa, thông báo vận hành thuần tuý.\n")
	sb.WriteString("- Nội dung kỹ thuật, hướng dẫn sử dụng, cẩm nang nghề không nêu tên cơ sở nào khác.\n\n")

	sb.WriteString("Nguyên tắc phân xử: gắn cờ cái RÕ RÀNG vi phạm, không gắn cờ cái chỉ hơi giống.\n")
	sb.WriteString("Một cổng chặn nhầm mọi thứ thì người duyệt sẽ bỏ qua nó, và kho lại nhận đúng thứ nó phải chặn.\n\n")

	sb.WriteString("## TÀI LIỆU CẦN THẨM ĐỊNH\n")
	for _, i := range indexes {
		page := pages[i]
		sb.WriteString(fmt.Sprintf("\n### index %d\n", i))
		sb.WriteString("Tiêu đề: " + stringFromMap(page, "title") + "\n")
		if url := stringFromMap(page, "url"); url != "" {
			sb.WriteString("Nguồn: " + url + "\n")
		}
		sb.WriteString("Nội dung:\n")
		sb.WriteString(knowledgeScreenExcerpt(stringFromMap(page, "markdown")))
		sb.WriteString("\n")
	}

	sb.WriteString("\n## ĐẦU RA\n")
	sb.WriteString("Trả lời CHỈ bằng một object JSON, không kèm giải thích, không bọc trong văn xuôi:\n")
	sb.WriteString(`{"ket_qua":[{"index":<số>,"quyet_dinh":"CHAP_NHAN|CHAP_NHAN_SAU_KHI_SUA|TU_CHOI",`)
	sb.WriteString(`"co_loai_tru":[{"ma":"E1","trich_doan":"","vi_tri":""}],`)
	sb.WriteString(`"so_lieu_can_dua_vao_CLAIM_MASTER":[{"noi_dung":"","gia_tri":"","can_tai_lieu_gi":""}],`)
	sb.WriteString(`"mau_thuan_phat_hien":[{"du_kien":"","gia_tri_A":"","gia_tri_B":"","nguon_A":"","nguon_B":""}],`)
	sb.WriteString(`"dung_lam_gi":"hoc_giong_van|tra_cuu_du_kien|ca_hai|khong_dung",`)
	sb.WriteString(`"ghi_chu_cho_nguoi_duyet":""}]}` + "\n\n")
	sb.WriteString("Phải có đúng một phán quyết cho MỖI index nêu trên.\n")
	sb.WriteString("`trich_doan` chép NGUYÊN VĂN đoạn gây ra cờ, không diễn giải lại.\n")
	sb.WriteString("Nếu quyet_dinh = TU_CHOI, KHÔNG đề xuất \"nạp phần còn lại\": tài liệu bị từ chối thì từ chối cả tài liệu, vì agent học theo văn phong tổng thể chứ không đọc theo đoạn.\n")
	sb.WriteString("Không gọi bất kỳ tool nào. Trả lời bằng JSON dạng văn bản thuần.\n")
	return sb.String()
}

// parseKnowledgeScreenReply bóc JSON ra khỏi câu trả lời. Model hay bọc object
// trong fence hoặc văn xuôi, nên cắt từ '{' đầu tới '}' cuối thay vì Unmarshal
// thẳng.
func parseKnowledgeScreenReply(reply string) (*knowledgeScreenReply, error) {
	object, err := extractJSONObject(reply)
	if err != nil {
		return nil, err
	}
	var parsed knowledgeScreenReply
	if err := decodeJSONInto(object, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func normalizeScreenDecision(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case knowledgeScreenAccept:
		return knowledgeScreenAccept
	case knowledgeScreenAcceptFix:
		return knowledgeScreenAcceptFix
	default:
		// Mặc định TỪ CHỐI: một giá trị lạ là không-vượt-qua-kiểm-tra.
		return knowledgeScreenReject
	}
}

// verdictToMap giữ đúng tên trường tiếng Việt của tài liệu để Drupal và panel
// đọc thẳng, không cần bảng ánh xạ thứ hai.
func verdictToMap(v knowledgeScreenVerdict) map[string]any {
	exclusions := make([]map[string]any, 0, len(v.Exclusions))
	for _, e := range v.Exclusions {
		code := strings.ToUpper(strings.TrimSpace(e.Code))
		if code == "" {
			continue
		}
		exclusions = append(exclusions, map[string]any{
			"ma": code, "trich_doan": strings.TrimSpace(e.Excerpt), "vi_tri": strings.TrimSpace(e.Where),
		})
	}
	claims := make([]map[string]any, 0, len(v.Claims))
	for _, c := range v.Claims {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		claims = append(claims, map[string]any{
			"noi_dung": strings.TrimSpace(c.Content), "gia_tri": strings.TrimSpace(c.Value),
			"can_tai_lieu_gi": strings.TrimSpace(c.NeedsDoc),
		})
	}
	conflicts := make([]map[string]any, 0, len(v.Conflicts))
	for _, c := range v.Conflicts {
		if strings.TrimSpace(c.Fact) == "" {
			continue
		}
		conflicts = append(conflicts, map[string]any{
			"du_kien": strings.TrimSpace(c.Fact), "gia_tri_A": strings.TrimSpace(c.ValueA),
			"gia_tri_B": strings.TrimSpace(c.ValueB), "nguon_A": strings.TrimSpace(c.SourceA),
			"nguon_B": strings.TrimSpace(c.SourceB),
		})
	}
	return map[string]any{
		"quyet_dinh":                       v.Decision,
		"co_loai_tru":                      exclusions,
		"so_lieu_can_dua_vao_CLAIM_MASTER": claims,
		"mau_thuan_phat_hien":              conflicts,
		"dung_lam_gi":                      strings.TrimSpace(v.UseFor),
		"ghi_chu_cho_nguoi_duyet":          strings.TrimSpace(v.Note),
	}
}

// rejectedVerdict là phán quyết mặc định khi model không nói gì về một trang.
func rejectedVerdict(note string) map[string]any {
	return map[string]any{
		"quyet_dinh":                       knowledgeScreenReject,
		"co_loai_tru":                      []map[string]any{},
		"so_lieu_can_dua_vao_CLAIM_MASTER": []map[string]any{},
		"mau_thuan_phat_hien":              []map[string]any{},
		"dung_lam_gi":                      "khong_dung",
		"ghi_chu_cho_nguoi_duyet":          note,
	}
}

// screenKnowledgePages chấm một lô và trả verdict theo index trang.
func (s *JobService) screenKnowledgePages(
	ctx context.Context,
	loop agent.Agent,
	job *store.TekshotJob,
	pages []map[string]any,
	indexes []int,
) (map[int]map[string]any, error) {
	userID := "tekshot-" + job.ExternalUserID
	runID := uuid.NewString()
	runCtx, cancel := context.WithTimeout(ctx, knowledgeScreenTimeout)
	defer cancel()

	result, err := loop.Run(runCtx, agent.RunRequest{
		// Session riêng mỗi lô: dùng lại session sẽ tích luỹ phán quyết cũ và
		// làm lệch lô sau.
		SessionKey:  job.SessionKey + ":kx-screen:" + runID,
		Message:     buildKnowledgeScreenPrompt(pages, indexes),
		Channel:     "tekshot_job",
		ChannelType: "tekshot",
		ChatID:      userID,
		PeerKind:    "direct",
		Addressed:   true,
		RunID:       runID,
		UserID:      userID,
		SenderID:    userID,
		// Sentinel, không phải tool thật — xem knowledgeScreenNoTools.
		ToolAllow:     []string{knowledgeScreenNoTools},
		MaxIterations: 1,
		SkillFilter:   []string{},
		LightContext:  true,
		HistoryLimit:  1,
		TraceName:     "tekshot knowledge screen",
		TraceTags:     []string{"tekshot", "knowledge_extract", "screen"},
	})
	if err != nil || result == nil {
		reason := "agent returned no result"
		if err != nil {
			reason = err.Error()
		}
		return nil, fmt.Errorf("%s", reason)
	}

	parsed, err := parseKnowledgeScreenReply(result.Content)
	if err != nil {
		return nil, err
	}

	out := make(map[int]map[string]any, len(indexes))
	wanted := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		wanted[i] = true
	}
	for _, v := range parsed.Results {
		if !wanted[v.Index] {
			continue
		}
		v.Decision = normalizeScreenDecision(v.Decision)
		out[v.Index] = verdictToMap(v)
	}
	return out, nil
}

// applyKnowledgeScreening chạy Prompt E trên report của cả hai nhánh (website
// và file) rồi lọc pages[]. Trang không được chấp nhận KHÔNG đi tiếp về Drupal
// dưới dạng tài liệu vault — nó nằm trong khối `screening.held` để người duyệt
// xem lý do và quyết định.
//
// Thất bại của lượt chấm là hỏng hóc, không phải phán quyết: cả lô bị giữ lại
// với status="failed", không bị coi là bẩn và cũng không lọt vào vault.
func (s *JobService) applyKnowledgeScreening(
	ctx context.Context,
	job *store.TekshotJob,
	request map[string]any,
	report map[string]any,
) map[string]any {
	if report == nil {
		return report
	}
	// Đường override từ Drupal: người duyệt đã xem lý do và vẫn quyết định nạp.
	// Drupal ghi log ai bấm; ở đây chỉ ghi nhận đã bỏ qua.
	if boolFromMap(request, "skip_screening") {
		report["screening"] = map[string]any{
			"status": "skipped", "reason": "Người duyệt yêu cầu nạp không qua thẩm định.",
			"accepted": len(anySlice(report["pages"])), "held": 0, "held_pages": []map[string]any{},
		}
		return report
	}

	rawPages := anySlice(report["pages"])
	if len(rawPages) == 0 {
		return report
	}
	pages := make([]map[string]any, 0, len(rawPages))
	for _, item := range rawPages {
		if entry, ok := item.(map[string]any); ok {
			pages = append(pages, entry)
		}
	}
	if len(pages) == 0 {
		return report
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		slog.Warn("tekshot: knowledge screening unavailable, holding the whole import",
			"job", job.ID.String(), "external", job.ExternalJobUUID, "reason", err.Error())
		return holdWholeImport(report, pages, "Không mở được agent để thẩm định: "+err.Error())
	}

	batches := knowledgeScreenBatches(pages)
	verdicts := make(map[int]map[string]any, len(pages))
	var failures []string
	for n, batch := range batches {
		s.reportKnowledgeProgress(ctx, job, fmt.Sprintf("Đang thẩm định tài liệu %d/%d lô", n+1, len(batches)))
		got, err := s.screenKnowledgePages(ctx, loop, job, pages, batch)
		if err != nil {
			slog.Warn("tekshot: knowledge screening batch failed",
				"job", job.ID.String(), "external", job.ExternalJobUUID, "batch", n+1, "reason", err.Error())
			failures = append(failures, err.Error())
			continue
		}
		for i, v := range got {
			verdicts[i] = v
		}
	}

	accepted := make([]map[string]any, 0, len(pages))
	held := make([]map[string]any, 0)
	for i, page := range pages {
		verdict, ok := verdicts[i]
		if !ok {
			// Mặc định TỪ CHỐI khi model bỏ sót một trang.
			verdict = rejectedVerdict("Lượt thẩm định không trả phán quyết cho trang này.")
		}
		decision := stringFromMap(verdict, "quyet_dinh")
		if decision == knowledgeScreenAccept {
			page["screening"] = verdict
			accepted = append(accepted, page)
			continue
		}
		held = append(held, map[string]any{
			"url": stringFromMap(page, "url"), "title": stringFromMap(page, "title"),
			"screening": verdict,
		})
	}

	urls := make([]string, 0, len(accepted))
	for _, page := range accepted {
		urls = append(urls, stringFromMap(page, "url"))
	}
	report["pages"] = accepted
	report["pages_fetched"] = urls

	status := "ok"
	if len(failures) > 0 {
		status = "failed"
	}
	screening := map[string]any{
		"status": status, "accepted": len(accepted), "held": len(held), "held_pages": held,
		"batches": len(batches),
		// Thẩm định đọc phần đầu mỗi trang — ghi lại để không ai đọc kết quả
		// này như đã soát toàn văn.
		"excerpt_chars": knowledgeScreenExcerptChars,
		// E10 ở đợt này chỉ soát mâu thuẫn TRONG lô đang nạp; đối chiếu với
		// tài liệu đã có trong vault cần vault_search, mâu thuẫn với thiết kế
		// tool-free của lượt chấm.
		"e10_scope": "batch",
	}
	if len(failures) > 0 {
		screening["reason"] = strings.Join(failures, " | ")
	}
	report["screening"] = screening

	// Không còn trang nào đi tiếp: report vẫn hợp lệ nhưng phải nói rõ là rỗng,
	// nếu không Drupal sẽ dựng một import trống mà không ai biết vì sao.
	if len(accepted) == 0 {
		report["status"] = "empty"
		if status == "failed" {
			report["reason"] = "Thẩm định lỗi, không trang nào được nạp."
		} else {
			report["reason"] = fmt.Sprintf("Thẩm định từ chối toàn bộ %d trang.", len(held))
		}
	}
	return report
}

// holdWholeImport giữ lại mọi trang khi lượt chấm không chạy được.
func holdWholeImport(report map[string]any, pages []map[string]any, reason string) map[string]any {
	held := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		held = append(held, map[string]any{
			"url": stringFromMap(page, "url"), "title": stringFromMap(page, "title"),
			"screening": rejectedVerdict(reason),
		})
	}
	report["pages"] = []map[string]any{}
	report["pages_fetched"] = []string{}
	report["status"] = "empty"
	report["reason"] = reason
	report["screening"] = map[string]any{
		"status": "failed", "reason": reason, "accepted": 0, "held": len(held),
		"held_pages": held, "batches": 0,
		"excerpt_chars": knowledgeScreenExcerptChars, "e10_scope": "batch",
	}
	return report
}

func boolFromMap(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	switch v := values[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}
