package tekshot

import (
	"strings"
	"testing"
)

func TestApplicableChecksFiltersByProfile(t *testing.T) {
	p7 := applicableChecks([]string{"P7"})
	joined := strings.Join(p7, "\n")
	for _, code := range []string{"C19", "C20", "C21", "C22"} {
		if !strings.Contains(joined, code+".") {
			t.Fatalf("P7 phải mang %s", code)
		}
	}
	for _, code := range []string{"C11", "C14", "C17"} {
		if strings.Contains(joined, code+".") {
			t.Fatalf("P7 không được mang %s (của profile khác)", code)
		}
	}
	// C1–C5 không gắn profile nên áp cho mọi trang.
	for _, code := range []string{"C1", "C2", "C3", "C4", "C5"} {
		if !strings.Contains(joined, code+".") {
			t.Fatalf("%s là luật chung, phải áp cho mọi profile", code)
		}
	}
}

func TestApplicableChecksUnionForTwoProfiles(t *testing.T) {
	joined := strings.Join(applicableChecks([]string{"P4", "P6"}), "\n")
	for _, code := range []string{"C11", "C12", "C13", "C17", "C18"} {
		if !strings.Contains(joined, code+".") {
			t.Fatalf("trang mang hai profile phải nhận HỢP các check, thiếu %s", code)
		}
	}
}

func TestNormalizeVerdictBlockWhenBlockersExist(t *testing.T) {
	// Kết luận không được mâu thuẫn với chính danh sách nó vừa liệt kê.
	reply := &complianceReply{
		Verdict:  verdictAllow,
		Blockers: []complianceBlocker{{Code: "C5", Excerpt: "giàu canxi"}},
	}
	if got := normalizeVerdict(reply); got != verdictBlock {
		t.Fatalf("có lỗi chặn thì phải CHAN dù model nói %q, got %q", verdictAllow, got)
	}
}

func TestNormalizeVerdictWarningsDowngradeToFix(t *testing.T) {
	reply := &complianceReply{
		Verdict:  verdictAllow,
		Warnings: []complianceWarning{{Code: "W1", Content: "thiếu thời hạn"}},
	}
	if got := normalizeVerdict(reply); got != verdictFix {
		t.Fatalf("có cảnh báo thì không được DUOC_DANG thẳng, got %q", got)
	}
}

func TestNormalizeVerdictUnknownIsNotAllow(t *testing.T) {
	for _, raw := range []string{"", "OK", "ĐƯỢC", "PASS"} {
		if got := normalizeVerdict(&complianceReply{Verdict: raw}); got == verdictAllow {
			t.Fatalf("kết luận lạ %q không được đọc thành đạt", raw)
		}
	}
}

func TestNormalizeVerdictCleanIsAllow(t *testing.T) {
	if got := normalizeVerdict(&complianceReply{Verdict: verdictAllow}); got != verdictAllow {
		t.Fatalf("sạch cả hai nhóm phải là DUOC_DANG, got %q", got)
	}
}

func TestParseComplianceReplyUnwrapsProse(t *testing.T) {
	reply := "Kết quả:\n```json\n{\"ket_luan\":\"CHAN\",\"loi_chan\":[{\"ma\":\"c5\",\"trich_doan\":\"giàu canxi\",\"sua_the_nao\":\"bỏ vế\"}]}\n```"
	parsed, err := parseComplianceReply(reply)
	if err != nil {
		t.Fatalf("parse lỗi: %v", err)
	}
	got := complianceResultToMap(parsed)
	if got["ket_luan"] != verdictBlock {
		t.Fatalf("ket_luan = %v", got["ket_luan"])
	}
	blockers := got["loi_chan"].([]map[string]any)
	if len(blockers) != 1 || blockers[0]["ma"] != "C5" {
		t.Fatalf("mã lỗi phải chuẩn hoá thành hoa: %+v", blockers)
	}
}

func TestBuildCompliancePromptForbidsSelfEdit(t *testing.T) {
	profile := pageProfileFromRequest(governedProfile())
	prompt := buildCompliancePrompt(profile, map[string]any{
		"title": "Bài test", "content": "nội dung",
		"tu_cham_risk": "LOW", "ly_do_cham_risk": "cẩm nang",
	})
	if !strings.Contains(prompt, "KHÔNG được tự sửa bài") {
		t.Fatal("overlay chỉ nêu chỗ sai, người quyết")
	}
	if !strings.Contains(prompt, "Công dụng chữa bệnh") {
		t.Fatal("prompt phải mang cấm riêng của trang")
	}
	if !strings.Contains(prompt, "chặn nhầm mọi thứ") {
		t.Fatal("phải có nguyên tắc chống dương tính giả — bài học từ Prompt E")
	}
}

func TestApplyComplianceOverlaySkipsInformationOnlyPages(t *testing.T) {
	// Trang chỉ được đăng bài thông tin thì không mang nghĩa vụ quảng cáo.
	profile := pageProfileFromRequest(governedProfile())
	batch := map[string]any{"posts": []map[string]any{{"title": "x", "content": "y"}}}
	applyComplianceOverlay(t.Context(), nil, "s", "u", profile, batch)
	post := batch["posts"].([]map[string]any)[0]
	if _, ok := post["compliance"]; ok {
		t.Fatal("bài của trang chỉ-thông-tin không được gọi overlay")
	}
}

func TestApplyComplianceOverlaySkipsExceptionPosts(t *testing.T) {
	raw := governedProfile()
	raw["page_profile"].(map[string]any)["muc_dich_cho_phep"] = []any{"THONG_TIN", "THUONG_MAI"}
	profile := pageProfileFromRequest(raw)
	batch := map[string]any{"posts": []map[string]any{{
		"title": "x", "content": "",
		"exception": map[string]any{"lan_ranh": "R13"},
	}}}
	applyComplianceOverlay(t.Context(), nil, "s", "u", profile, batch)
	post := batch["posts"].([]map[string]any)[0]
	if _, ok := post["compliance"]; ok {
		t.Fatal("bài treo lằn ranh chưa có gì để soát")
	}
}

func TestApplyComplianceOverlayNoopWithoutProfile(t *testing.T) {
	batch := map[string]any{"posts": []map[string]any{{"title": "x", "content": "y"}}}
	applyComplianceOverlay(t.Context(), nil, "s", "u", nil, batch)
	if _, ok := batch["posts"].([]map[string]any)[0]["compliance"]; ok {
		t.Fatal("trang chưa bật luật thì overlay phải im lặng hoàn toàn")
	}
}

func TestImageGuidanceEmptyWithoutProfile(t *testing.T) {
	if imageGuidanceFor(map[string]any{"loai_anh": "PRODUCT"}) != "" {
		t.Fatal("trang chưa bật luật thì lượt sinh ảnh không đổi một ký tự")
	}
}

func TestImageGuidanceForbidsProductMockups(t *testing.T) {
	guidance := imageGuidanceFor(governedProfile())
	for _, phrase := range []string{
		"Món ăn, đồ uống đang bán",
		"Giao diện phần mềm",
		"Chân dung người",
		"Logo, nhãn hiệu bất kỳ",
		"chỉnh kỹ thuật, không đổi bản chất",
	} {
		if !strings.Contains(guidance, phrase) {
			t.Fatalf("luật ảnh thiếu %q", phrase)
		}
	}
}

func TestMediaBranchRulesBlockGenerationForRealPhotos(t *testing.T) {
	for _, branch := range []string{"REAL", "PRODUCT", "real", "product"} {
		rules := mediaBranchRules(branch)
		if !strings.Contains(rules, "KHÔNG sinh ảnh") {
			t.Fatalf("nhánh %q phải cấm sinh ảnh", branch)
		}
		if !strings.Contains(rules, "brief chụp") {
			t.Fatalf("nhánh %q phải yêu cầu brief chụp thay vì sinh ảnh", branch)
		}
	}
	if !strings.Contains(mediaBranchRules("INFO"), "Đồ họa tạo bằng AI") {
		t.Fatal("infographic phải mang nhãn nguồn")
	}
	if mediaBranchRules("AI") != "" {
		t.Fatal("nhánh AI đã bị lớp cấm chung phủ, không cần luật riêng")
	}
}
