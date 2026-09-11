package tekshot

import (
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func imageReviewRequest() map[string]any {
	return map[string]any{
		"page_name":        "Tekshot OS",
		"page_description": "Camera AI, POS, Marketing AI",
		"post_title":       "THIẾT LẬP VÀ QUẢN LÝ CHƯƠNG TRÌNH KHUYẾN MÃI",
		"post_brief":       "Khuyến mãi linh hoạt trên POS",
		"post_content":     "Tạo voucher, quản lý mã giảm giá và đo lường hiệu quả từng chiến dịch trên cùng một hệ thống.",
		"source_row":       "Title: Khuyến mãi linh hoạt",
	}
}

func TestImageReviewOnlyLooksAtTheImage(t *testing.T) {
	allow := imageReviewToolAllow()
	if len(allow) != 1 || allow[0] != "read_image" {
		t.Fatalf("reviewer must only be able to read the image, got %v", allow)
	}
}

func TestImageReviewPromptCarriesThePostAndTheChecks(t *testing.T) {
	prompt := buildImageReviewPrompt(imageReviewRequest())
	for _, want := range []string{
		"THIẾT LẬP VÀ QUẢN LÝ CHƯƠNG TRÌNH KHUYẾN MÃI",
		"đo lường hiệu quả từng chiến dịch",
		"Title: Khuyến mãi linh hoạt",
		"Tekshot OS",
		"chu_trong_anh",
		"biển hiệu",
		"THONG_DIEP", "CHU", "PHUONG_TIEN", "SAN_PHAM", "NOI_QUA", "KY_THUAT",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	// Reviewer không được thấy prompt sinh ảnh — chỉ thấy bài và ảnh.
	if strings.Contains(prompt, "create_image") {
		t.Error("reviewer prompt must not carry the generator's instructions")
	}
}

func TestCleanImagePasses(t *testing.T) {
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["Tạo voucher","Đo lường hiệu quả"],"loi":[],"ghi_chu":"ổn"}`, imageReviewPostText(imageReviewRequest()))
	if got["ket_luan"] != reviewVerdictPass {
		t.Fatalf("clean image should pass, got %v", got)
	}
	if len(got["loi"].([]map[string]any)) != 0 {
		t.Fatalf("clean image should carry no defects, got %v", got["loi"])
	}
}

func TestTextNotInThePostIsFlaggedEvenWhenTheModelMissesIt(t *testing.T) {
	// Đo thật: ảnh cửa hàng tự thêm biển "Good Food Brighter Days" và QA cũ cho qua.
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["Tạo voucher","Good Food Brighter Days"],"loi":[]}`, imageReviewPostText(imageReviewRequest()))
	if got["ket_luan"] != reviewVerdictWarn {
		t.Fatalf("invented text must warn, got %v", got)
	}
	defects := got["loi"].([]map[string]any)
	if len(defects) != 1 || defects[0]["loai"] != "CHU" || !strings.Contains(defects[0]["chi_tiet"].(string), "Good Food Brighter Days") {
		t.Fatalf("expected one CHU defect naming the invented text, got %v", defects)
	}
}

func TestDrawnEmojiOrQuotesAreFlaggedEvenWhenTheTextIsInThePost(t *testing.T) {
	// Đo thật: ảnh khuôn tiêu đề vẽ cả 📊 và ngoặc kép, người soát vẫn cho DAT.
	request := imageReviewRequest()
	request["post_title"] = "📊 QUẢN LÝ CHUỖI 3 CỬA HÀNG"
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["\"📊 QUẢN LÝ CHUỖI","3 CỬA HÀNG\""],"loi":[]}`, imageReviewPostText(request))
	if got["ket_luan"] != reviewVerdictWarn {
		t.Fatalf("drawn emoji or quotes must warn, got %v", got)
	}
	defects := got["loi"].([]map[string]any)
	if len(defects) != 1 || defects[0]["loai"] != "CHU" || !strings.HasPrefix(defects[0]["chi_tiet"].(string), "Vẽ cả emoji hoặc dấu ngoặc kép") {
		t.Fatalf("expected one merged CHU defect for drawn marks, got %v", defects)
	}
}

func TestMissingWordsAreMergedIntoOneWarning(t *testing.T) {
	// Đo thật: người soát chép biển hiệu từng từ một — 4 dòng cảnh báo cho 1 biển.
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["Good","Food","Brighter","Days","Tạo voucher"],"loi":[]}`, imageReviewPostText(imageReviewRequest()))
	defects := got["loi"].([]map[string]any)
	if len(defects) != 1 {
		t.Fatalf("missing words must be one warning, got %v", defects)
	}
	want := `Không có trong bài: "Good", "Food", "Brighter", "Days".`
	if defects[0]["chi_tiet"] != want {
		t.Fatalf("got %q, want %q", defects[0]["chi_tiet"], want)
	}
}

func TestReviewPromptAsksToKeepEveryCharacter(t *testing.T) {
	if !strings.Contains(buildImageReviewPrompt(imageReviewRequest()), "giữ nguyên dấu ngoặc kép, emoji") {
		t.Fatal("transcription must keep quotes and emoji or the code check sees nothing")
	}
}

func TestTextMatchIgnoresCaseAndPunctuation(t *testing.T) {
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["ĐO LƯỜNG HIỆU QUẢ!", "  tạo   voucher "],"loi":[]}`, imageReviewPostText(imageReviewRequest()))
	if got["ket_luan"] != reviewVerdictPass {
		t.Fatalf("case and punctuation must not count as invented text, got %v", got)
	}
}

func TestTextMatchIgnoresUnicodeNormalisation(t *testing.T) {
	// "hiệu quả" viết bằng dấu tổ hợp (NFD) — dữ liệu Việt ở đây lẫn cả hai dạng.
	decomposed := "học hiệu quả"
	if norm.NFC.String(decomposed) == decomposed {
		t.Fatal("fixture must be decomposed")
	}
	request := imageReviewRequest()
	request["post_content"] = norm.NFC.String("Bài học hiệu quả.")
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":["`+decomposed+`"],"loi":[]}`, imageReviewPostText(request))
	if got["ket_luan"] != reviewVerdictPass {
		t.Fatalf("NFD text must match the NFC post, got %v", got)
	}
}

func TestPassWithListedDefectsBecomesWarning(t *testing.T) {
	got := reviewImageReply(`{"ket_luan":"DAT","chu_trong_anh":[],"loi":[{"loai":"KY_THUAT","vi_tri":"tay phải","chi_tiet":"sáu ngón"}]}`, imageReviewPostText(imageReviewRequest()))
	if got["ket_luan"] != reviewVerdictWarn {
		t.Fatalf("a verdict must not contradict its own defect list, got %v", got)
	}
}

func TestUnreadableReplyIsAWarningNotAPass(t *testing.T) {
	for _, reply := range []string{"", "Ảnh đẹp, không có vấn đề.", `{"ket_luan":"MAYBE","loi":[]}`} {
		got := reviewImageReply(reply, imageReviewPostText(imageReviewRequest()))
		if got["ket_luan"] != reviewVerdictWarn {
			t.Fatalf("reply %q must fail closed, got %v", reply, got)
		}
		if len(got["loi"].([]map[string]any)) == 0 {
			t.Fatalf("reply %q must say why it warns", reply)
		}
	}
}

func TestDefectsWithoutDetailAreDropped(t *testing.T) {
	got := reviewImageReply(`{"ket_luan":"CANH_BAO","chu_trong_anh":[],"loi":[{"loai":"THONG_DIEP","vi_tri":"","chi_tiet":"  "},{"loai":"thong_diep","vi_tri":"toàn ảnh","chi_tiet":"không thấy khuyến mãi"}]}`, imageReviewPostText(imageReviewRequest()))
	defects := got["loi"].([]map[string]any)
	if len(defects) != 1 || defects[0]["loai"] != "THONG_DIEP" {
		t.Fatalf("empty defects are dropped and codes upper-cased, got %v", defects)
	}
}
