package tekshot

import (
	"strings"
	"testing"
)

func postProofreadRequest() map[string]any {
	return map[string]any{
		"title":    "Pizza hải sản giảm gía cuối tuần",
		"content":  "Cuối tuần này ,pizza hải sản chỉ 199.000đ 🍕 Đặt ngay ko lo hết bánh! #PizzaVinhPhuc https://pizza.vn",
		"hashtags": "#PizzaVinhPhuc",
	}
}

func TestPostProofreadIsSupportedJobType(t *testing.T) {
	if !isSupportedTekshotJobType(TekshotJobTypePostProofread) {
		t.Fatalf("post_proofread must be accepted by Create")
	}
}

func TestPostProofreadPromptCarriesRules(t *testing.T) {
	prompt := buildPostProofreadPrompt(postProofreadRequest())
	for _, fragment := range []string{
		"dấu thanh",
		"Viết hoa đúng tên riêng",
		"KHÔNG đổi giọng thương hiệu",
		"KHÔNG bịa",
		"GIỮ NGUYÊN hashtag, emoji, link",
		"NGUYÊN VĂN",
		"improvements",
		"Pizza hải sản giảm gía cuối tuần",
		"#PizzaVinhPhuc",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("prompt missing %q", fragment)
		}
	}
}

func TestPostProofreadDropsSuggestionsNotInPost(t *testing.T) {
	reply := "Đây là kết quả:\n```json\n" + `{"suggestions":[
		{"type":"chinh_ta","field":"title","original":"giảm gía","suggestion":"giảm giá","reason":"sai dấu"},
		{"type":"dau_cau","field":"content","original":"này ,pizza","suggestion":"này, pizza","reason":"khoảng trắng"},
		{"type":"chinh_ta","field":"content","original":"câu không hề có","suggestion":"x","reason":"bịa"},
		{"type":"chinh_ta","field":"title","original":"ko lo","suggestion":"không lo","reason":"viết tắt, model ghi nhầm ô"},
		{"type":"spam","field":"content","original":"Đặt ngay","suggestion":"Mua","reason":"loại lạ"},
		{"type":"tu_ngu","field":"content","original":"Đặt ngay","suggestion":"Đặt ngay","reason":"không đổi gì"}
	],"improvements":[
		{"kind":"hook","suggestion":"Cuối tuần thèm pizza?","reason":"mở bằng câu hỏi"},
		{"kind":"lạ","suggestion":"Gọi ngay","reason":""},
		{"kind":"cta","suggestion":"","reason":"rỗng"},
		{"kind":"cta","suggestion":"Inbox để giữ bánh","reason":""},
		{"kind":"cta","suggestion":"Thừa","reason":"quá 3"}
	]}` + "\n```"
	parsed, err := parsePostProofreadReply(reply, postProofreadRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	suggestions := parsed["suggestions"].([]any)
	if len(suggestions) != 3 {
		t.Fatalf("want 3 kept suggestions, got %d: %#v", len(suggestions), suggestions)
	}
	moved := suggestions[2].(map[string]any)
	if moved["field"] != "content" || moved["original"] != "ko lo" {
		t.Fatalf("wrong-field suggestion should move to content: %#v", moved)
	}
	if parsed["dropped"] != 3 {
		t.Fatalf("want 3 dropped, got %v", parsed["dropped"])
	}
	improvements := parsed["improvements"].([]any)
	if len(improvements) != 3 {
		t.Fatalf("want 3 improvements, got %d", len(improvements))
	}
	if improvements[1].(map[string]any)["kind"] != "khac" {
		t.Fatalf("unknown kind should become khac: %#v", improvements[1])
	}
}

func TestPostProofreadRejectsBrokenJSON(t *testing.T) {
	for _, reply := range []string{"không có json", `{"suggestions": "sai kiểu"}`} {
		if _, err := parsePostProofreadReply(reply, postProofreadRequest()); err == nil {
			t.Fatalf("accepted invalid reply %q", reply)
		}
	}
	parsed, err := parsePostProofreadReply(`{"suggestions":[],"improvements":[]}`, postProofreadRequest())
	if err != nil || len(parsed["suggestions"].([]any)) != 0 {
		t.Fatalf("clean post must parse to empty suggestions: %v", err)
	}
}
