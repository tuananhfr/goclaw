package tekshot

import (
	"strings"
	"testing"
)

func governedProfile() map[string]any {
	return map[string]any{
		"page_profile": map[string]any{
			"profile":            []any{"P2", "P4"},
			"muc_dich_cho_phep":  []any{"THONG_TIN"},
			"dinh_dang_cho_phep": []any{"F1", "F7"},
			"cam_rieng":          []any{"Công dụng chữa bệnh, phòng bệnh, hỗ trợ điều trị."},
			"bat_buoc_rieng":     []any{"Mỗi bài ghi số công bố sản phẩm."},
			"chu_de_bi_cam":      []any{"nhận diện khuôn mặt"},
			"cam_cta_thu_sdt":    true,
			"phap_nhan":          "Công ty FOODTEK",
		},
	}
}

func TestPageProfileFromRequestNilWhenAbsent(t *testing.T) {
	if pageProfileFromRequest(map[string]any{}) != nil {
		t.Fatal("không có page_profile thì phải trả nil — đó là tín hiệu chạy luật cũ")
	}
	if pageProfileFromRequest(map[string]any{"page_profile": map[string]any{}}) != nil {
		t.Fatal("page_profile rỗng cũng phải trả nil")
	}
	if pageProfileFromRequest(map[string]any{"page_profile": map[string]any{"phap_nhan": "X"}}) != nil {
		t.Fatal("page_profile không có mã profile là không dùng được")
	}
}

func TestBuildGovernancePromptCarriesProfileRules(t *testing.T) {
	profile := pageProfileFromRequest(governedProfile())
	if profile == nil {
		t.Fatal("profile phải đọc được")
	}
	prompt := buildGovernancePrompt(profile)
	for _, phrase := range []string{
		"P2 + P4",
		"Công ty FOODTEK",
		"Công dụng chữa bệnh",
		"Mỗi bài ghi số công bố sản phẩm.",
		"nhận diện khuôn mặt",
		"CẤM mọi CTA thu thập số điện thoại",
		"ly_do_cham_risk",
		"TỐI THIỂU HAI phương án",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("prompt thiếu %q", phrase)
		}
	}
}

func TestBuildGovernancePromptEmptyWithoutProfile(t *testing.T) {
	if buildGovernancePrompt(nil) != "" {
		t.Fatal("page chưa bật luật thì prompt phải không đổi một ký tự")
	}
}

func TestNormalizeRiskDefaultsToHigh(t *testing.T) {
	cases := map[string]string{
		"LOW": riskLow, "low": riskLow, "MEDIUM": riskMedium,
		"HIGH": riskHigh, "": riskHigh, "chưa rõ": riskHigh, "SAFE": riskHigh,
	}
	for in, want := range cases {
		if got := normalizeRisk(in); got != want {
			t.Fatalf("normalizeRisk(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateGovernanceFieldsDemandsReason(t *testing.T) {
	err := validateGovernanceFields(map[string]any{
		"tu_cham_risk": "LOW", "khoi_4_gioi_han": "Giá áp dụng tới 30/9.",
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "ly_do_cham_risk") {
		t.Fatalf("thiếu lý do chấm phải báo lỗi, got %v", err)
	}
}

func TestValidateGovernanceFieldsDemandsLimitBlock(t *testing.T) {
	err := validateGovernanceFields(map[string]any{
		"tu_cham_risk": "LOW", "ly_do_cham_risk": "Bài cẩm nang, không số liệu.",
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "khoi_4_gioi_han") {
		t.Fatalf("thiếu khối giới hạn phải báo lỗi, got %v", err)
	}
}

func TestValidateGovernanceFieldsNormalizesRiskInPlace(t *testing.T) {
	post := map[string]any{
		"tu_cham_risk": "chưa rõ", "ly_do_cham_risk": "có tên khách hàng",
		"khoi_4_gioi_han": "Số liệu quý 2/2026.",
	}
	if err := validateGovernanceFields(post, 0); err != nil {
		t.Fatalf("không được lỗi: %v", err)
	}
	if post["tu_cham_risk"] != riskHigh {
		t.Fatalf("mức lạ phải đọc thành HIGH, got %v", post["tu_cham_risk"])
	}
}

func TestValidateGovernanceFieldsExceptionNeedsTwoOptions(t *testing.T) {
	err := validateGovernanceFields(map[string]any{
		"exception": map[string]any{
			"lan_ranh": "R13", "trich_doan": "giàu canxi cho trẻ nhỏ",
			"phuong_an": []any{"Bỏ vế dinh dưỡng"},
		},
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "hai phương án") {
		t.Fatalf("một phương án là chưa đủ — AI không được tự chọn, got %v", err)
	}
}

func TestValidateGovernanceFieldsExceptionNeedsExcerpt(t *testing.T) {
	err := validateGovernanceFields(map[string]any{
		"exception": map[string]any{
			"lan_ranh": "R13", "trich_doan": "  ",
			"phuong_an": []any{"Bỏ vế dinh dưỡng", "Viết lại thành F4 kiến thức"},
		},
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "trích nguyên văn") {
		t.Fatalf("exception phải trích nguyên văn, got %v", err)
	}
}

func TestValidateGovernanceFieldsExceptionSkipsReason(t *testing.T) {
	// Bài chưa viết thì không có gì để chấm lý do — exception hợp lệ là đủ.
	post := map[string]any{
		"exception": map[string]any{
			"lan_ranh": "R13", "trich_doan": "giàu canxi cho trẻ nhỏ",
			"phuong_an": []any{"Bỏ vế dinh dưỡng", "Viết lại thành F4 kiến thức"},
		},
	}
	if err := validateGovernanceFields(post, 0); err != nil {
		t.Fatalf("exception đủ điều kiện không được báo lỗi: %v", err)
	}
}

func TestGovernedPostAcceptsEmptyContentOnException(t *testing.T) {
	post := map[string]any{
		"title": "Dinh dưỡng pizza", "brief": "b", "pillar": "p",
		"content": "", "hashtags": "", "publish_at": "2026-06-18T08:30:00",
		"publish_date": "2026-06-18", "publish_time": "08:30",
		"checklist_item": "row",
		"exception": map[string]any{
			"lan_ranh": "R13", "trich_doan": "giàu canxi",
			"phuong_an": []any{"Bỏ vế dinh dưỡng", "Viết lại thành F4"},
		},
	}
	if _, err := validateDraftPost(post, 0, true); err != nil {
		t.Fatalf("bài chạm lằn ranh là kết quả hợp lệ, không phải lỗi: %v", err)
	}
}

func TestUngovernedPostStillRejectsEmptyContent(t *testing.T) {
	post := map[string]any{
		"title": "x", "brief": "b", "pillar": "p", "content": "", "hashtags": "",
		"publish_at": "2026-06-18T08:30:00", "publish_date": "2026-06-18",
		"publish_time": "08:30", "checklist_item": "row",
	}
	if _, err := validateDraftPost(post, 0, false); err == nil {
		t.Fatal("page chưa bật luật thì content rỗng vẫn phải là lỗi như cũ")
	}
}

func TestUngovernedPostRejectsGovernanceFields(t *testing.T) {
	post := map[string]any{
		"title": "x", "brief": "b", "pillar": "p", "content": "c", "hashtags": "",
		"publish_at": "2026-06-18T08:30:00", "publish_date": "2026-06-18",
		"publish_time": "08:30", "checklist_item": "row", "tu_cham_risk": "LOW",
	}
	if _, err := validateDraftPost(post, 0, false); err == nil {
		t.Fatal("page chưa bật luật không được nhận trường tuân thủ — hợp đồng cũ phải nguyên vẹn")
	}
}

func TestGovernedSchemaAddsRequiredFields(t *testing.T) {
	tool := NewDraftBatchCollectorTool().withProfile(pageProfileFromRequest(governedProfile()))
	items := tool.Parameters()["properties"].(map[string]any)["posts"].(map[string]any)["items"].(map[string]any)
	props := items["properties"].(map[string]any)
	for _, key := range []string{"tu_cham_risk", "ly_do_cham_risk", "exception", "media_brief", "lich_ra_soat"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("schema thiếu trường %q", key)
		}
	}
	required := items["required"].([]string)
	for _, key := range governanceRequiredFields() {
		found := false
		for _, item := range required {
			if item == key {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q phải nằm trong required", key)
		}
	}
}

func TestUngovernedSchemaUnchanged(t *testing.T) {
	tool := NewDraftBatchCollectorTool()
	items := tool.Parameters()["properties"].(map[string]any)["posts"].(map[string]any)["items"].(map[string]any)
	if _, ok := items["properties"].(map[string]any)["tu_cham_risk"]; ok {
		t.Fatal("page chưa bật luật thì schema phải y hệt bản cũ")
	}
}
