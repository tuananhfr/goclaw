package tekshot

import (
	"strings"
	"testing"
)

func TestKnowledgeScreenBatchesSplitsByPageCap(t *testing.T) {
	pages := make([]map[string]any, 0, 20)
	for i := 0; i < 20; i++ {
		pages = append(pages, map[string]any{"markdown": "ngắn"})
	}
	batches := knowledgeScreenBatches(pages)
	if len(batches) != 3 {
		t.Fatalf("batches = %d, want 3 (20 trang / trần %d)", len(batches), knowledgeScreenBatchPages)
	}
	seen := 0
	for _, b := range batches {
		if len(b) > knowledgeScreenBatchPages {
			t.Fatalf("lô %d trang, vượt trần %d", len(b), knowledgeScreenBatchPages)
		}
		seen += len(b)
	}
	if seen != 20 {
		t.Fatalf("tổng index = %d, want 20 — không trang nào được rơi", seen)
	}
}

func TestKnowledgeScreenBatchInputIsBounded(t *testing.T) {
	// Trang dài cỡ nào cũng không kéo lô vượt ngân sách, vì excerpt chặn trước.
	// Đây là bất biến thay cho một trần ký tự riêng.
	pages := make([]map[string]any, 0, knowledgeScreenBatchPages)
	for i := 0; i < knowledgeScreenBatchPages; i++ {
		pages = append(pages, map[string]any{"markdown": strings.Repeat("a", 500000)})
	}
	batches := knowledgeScreenBatches(pages)
	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}
	chars := 0
	for _, i := range batches[0] {
		chars += len([]rune(knowledgeScreenExcerpt(stringFromMap(pages[i], "markdown"))))
	}
	marker := len([]rune(knowledgeScreenExcerpt(strings.Repeat("a", knowledgeScreenExcerptChars+1)))) - knowledgeScreenExcerptChars
	budget := knowledgeScreenBatchPages * (knowledgeScreenExcerptChars + marker)
	if chars > budget {
		t.Fatalf("lô mang %d ký tự, vượt ngân sách %d", chars, budget)
	}
}

func TestKnowledgeScreenBatchesCoverEveryPageOnce(t *testing.T) {
	// 400 đoạn (trần một file) phải chia hết, không trang nào rơi hoặc lặp.
	pages := make([]map[string]any, 0, knowledgeFileMaxChunks)
	for i := 0; i < knowledgeFileMaxChunks; i++ {
		pages = append(pages, map[string]any{"markdown": "đoạn"})
	}
	seen := map[int]int{}
	for _, batch := range knowledgeScreenBatches(pages) {
		for _, i := range batch {
			seen[i]++
		}
	}
	if len(seen) != knowledgeFileMaxChunks {
		t.Fatalf("phủ %d trang, want %d", len(seen), knowledgeFileMaxChunks)
	}
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("trang %d xuất hiện %d lần", i, n)
		}
	}
}

func TestKnowledgeScreenBatchesEmpty(t *testing.T) {
	if got := knowledgeScreenBatches(nil); len(got) != 0 {
		t.Fatalf("không trang nào thì không có lô nào, got %v", got)
	}
}

func TestKnowledgeScreenExcerptCaps(t *testing.T) {
	got := knowledgeScreenExcerpt(strings.Repeat("x", knowledgeScreenExcerptChars+500))
	if !strings.HasSuffix(got, "…[cắt]") {
		t.Fatalf("đoạn dài phải được đánh dấu đã cắt, got tail %q", got[len(got)-20:])
	}
	short := "nội dung ngắn"
	if knowledgeScreenExcerpt(short) != short {
		t.Fatalf("đoạn ngắn không được đụng vào")
	}
}

func TestParseKnowledgeScreenReplyUnwrapsProse(t *testing.T) {
	reply := "Đây là kết quả:\n```json\n{\"ket_qua\":[{\"index\":0,\"quyet_dinh\":\"TU_CHOI\"," +
		"\"co_loai_tru\":[{\"ma\":\"E1\",\"trich_doan\":\"top 5 quán\",\"vi_tri\":\"tiêu đề\"}]}]}\n```\nHết."
	parsed, err := parseKnowledgeScreenReply(reply)
	if err != nil {
		t.Fatalf("parse lỗi: %v", err)
	}
	if len(parsed.Results) != 1 || parsed.Results[0].Decision != "TU_CHOI" {
		t.Fatalf("kết quả = %+v", parsed.Results)
	}
	if len(parsed.Results[0].Exclusions) != 1 || parsed.Results[0].Exclusions[0].Code != "E1" {
		t.Fatalf("loại trừ = %+v", parsed.Results[0].Exclusions)
	}
}

func TestParseKnowledgeScreenReplyRejectsGarbage(t *testing.T) {
	if _, err := parseKnowledgeScreenReply("không có json ở đây"); err == nil {
		t.Fatal("câu trả lời không có JSON phải báo lỗi, không được coi là đỗ")
	}
}

func TestNormalizeScreenDecisionDefaultsToReject(t *testing.T) {
	cases := map[string]string{
		"CHAP_NHAN":             knowledgeScreenAccept,
		"chap_nhan":             knowledgeScreenAccept,
		"CHAP_NHAN_SAU_KHI_SUA": knowledgeScreenAcceptFix,
		"TU_CHOI":               knowledgeScreenReject,
		"":                      knowledgeScreenReject,
		"MAYBE":                 knowledgeScreenReject,
		"ACCEPT":                knowledgeScreenReject,
	}
	for in, want := range cases {
		if got := normalizeScreenDecision(in); got != want {
			t.Fatalf("normalizeScreenDecision(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerdictToMapDropsEmptyEntries(t *testing.T) {
	got := verdictToMap(knowledgeScreenVerdict{
		Decision: knowledgeScreenAccept,
		Exclusions: []knowledgeScreenExclusion{
			{Code: " e3 ", Excerpt: " tốt nhất "}, {Code: ""},
		},
		Claims:    []knowledgeScreenClaim{{Content: "ủ bột"}, {Content: "  "}},
		Conflicts: []knowledgeScreenConflict{{Fact: ""}},
	})
	ex := got["co_loai_tru"].([]map[string]any)
	if len(ex) != 1 || ex[0]["ma"] != "E3" || ex[0]["trich_doan"] != "tốt nhất" {
		t.Fatalf("loại trừ = %+v, want một mục E3 đã trim + hoa", ex)
	}
	if len(got["so_lieu_can_dua_vao_CLAIM_MASTER"].([]map[string]any)) != 1 {
		t.Fatalf("claim rỗng phải bị bỏ")
	}
	if len(got["mau_thuan_phat_hien"].([]map[string]any)) != 0 {
		t.Fatalf("mâu thuẫn rỗng phải bị bỏ")
	}
}

func TestHoldWholeImportEmptiesPages(t *testing.T) {
	report := map[string]any{
		"status": "ok",
		"pages":  []any{map[string]any{"url": "https://a.vn/x", "title": "X"}},
	}
	pages := []map[string]any{{"url": "https://a.vn/x", "title": "X"}}
	got := holdWholeImport(report, pages, "agent chết")

	if len(got["pages"].([]map[string]any)) != 0 {
		t.Fatal("thẩm định hỏng thì không trang nào được đi tiếp vào vault")
	}
	if got["status"] != "empty" {
		t.Fatalf("status = %v, want empty", got["status"])
	}
	screening := got["screening"].(map[string]any)
	if screening["status"] != "failed" || screening["held"].(int) != 1 {
		t.Fatalf("screening = %+v", screening)
	}
	held := screening["held_pages"].([]map[string]any)
	if held[0]["screening"].(map[string]any)["quyet_dinh"] != knowledgeScreenReject {
		t.Fatal("trang bị giữ phải mang phán quyết TU_CHOI mặc định")
	}
}

func TestBoolFromMapReadsDrupalShapes(t *testing.T) {
	cases := []struct {
		value any
		want  bool
	}{
		{true, true}, {"true", true}, {"TRUE", true}, {"1", true}, {float64(1), true},
		{false, false}, {"false", false}, {"", false}, {float64(0), false}, {nil, false},
	}
	for _, c := range cases {
		if got := boolFromMap(map[string]any{"skip_screening": c.value}, "skip_screening"); got != c.want {
			t.Fatalf("boolFromMap(%#v) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestBuildKnowledgeScreenPromptCarriesAllTenRules(t *testing.T) {
	pages := []map[string]any{{"url": "https://a.vn/1", "title": "Top 5 quán pizza", "markdown": "nội dung"}}
	prompt := buildKnowledgeScreenPrompt(pages, []int{0})
	for _, code := range []string{"E1.", "E2.", "E3.", "E4.", "E5.", "E6.", "E7.", "E8.", "E9.", "E10."} {
		if !strings.Contains(prompt, code) {
			t.Fatalf("prompt thiếu tiêu chí %s", code)
		}
	}
	if !strings.Contains(prompt, "MẶC ĐỊNH: TỪ CHỐI") {
		t.Fatal("prompt phải nêu rõ mặc định là từ chối")
	}
	if !strings.Contains(prompt, "index 0") || !strings.Contains(prompt, "Top 5 quán pizza") {
		t.Fatal("prompt phải mang index và tiêu đề của từng trang")
	}
	if !strings.Contains(prompt, "Không gọi bất kỳ tool nào") {
		t.Fatal("prompt phải khoá tool bằng câu chữ, vì alsoAllow có thể union tool trở lại")
	}
}

func TestBuildKnowledgeScreenPromptCarriesNegativeExamples(t *testing.T) {
	// Không có khối phản ví dụ, model gắn cờ mọi danh từ riêng: đo trên
	// pizzahips.com cho 5/7 dương tính giả ở E6, đều là "phô mai mozzarella".
	prompt := buildKnowledgeScreenPrompt(
		[]map[string]any{{"url": "https://a.vn/1", "title": "Pizza 4 loại phô mai", "markdown": "phô mai mozzarella"}},
		[]int{0},
	)
	if !strings.Contains(prompt, "KHÔNG PHẢI LOẠI TRỪ") {
		t.Fatal("prompt phải có khối phản ví dụ")
	}
	for _, phrase := range []string{"mozzarella", "danh từ chung", "Bên thứ ba nghĩa là NGƯỜI KHÁC"} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("khối phản ví dụ thiếu %q", phrase)
		}
	}
	if !strings.Contains(prompt, "không gắn cờ cái chỉ hơi giống") {
		t.Fatal("prompt phải nêu nguyên tắc phân xử, nếu không cổng chặn nhầm sẽ bị bỏ qua")
	}
}
