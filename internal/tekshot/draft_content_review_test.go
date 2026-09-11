package tekshot

import (
	"strings"
	"testing"
)

func contentReviewInput() (map[string]any, string, string) {
	post := map[string]any{
		"title":   "QUÁN CAFE TĂNG 30% TỐC ĐỘ PHỤC VỤ GIỜ CAO ĐIỂM",
		"brief":   "QR order và KDS",
		"content": "Khách quét QR tại bàn, đơn vào thẳng màn hình KDS trong bếp.",
	}
	source := "Title: Quán cafe tăng 30% tốc độ phục vụ\nQR order, KDS, giảm gọi món sai"
	facts := "- KDS hiển thị đơn theo thứ tự thời gian"
	return post, source, facts
}

func TestContentReviewPromptCarriesThePostSourceAndScale(t *testing.T) {
	post, source, facts := contentReviewInput()
	prompt := buildContentReviewPrompt(post, source, facts)
	for _, want := range []string{
		"QUÁN CAFE TĂNG 30% TỐC ĐỘ PHỤC VỤ", "Khách quét QR tại bàn", "giảm gọi món sai", "KDS hiển thị đơn",
		`"diem"`, "sua_duoc", "7-8",
		"SU_THAT", "THIEU_Y", "CHUNG_CHUNG", "MO_BAI", "CTA", "META", "NGON_NGU",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestCleanPostPasses(t *testing.T) {
	post, source, facts := contentReviewInput()
	got := reviewContentReply(`{"ket_luan":"DAT","diem":8,"loi":[]}`, post, source, facts)
	if got["ket_luan"] != reviewVerdictPass || got["diem"] != 8 {
		t.Fatalf("clean post should pass at 8, got %v", got)
	}
}

func TestNumberWithoutASourceIsFlaggedByCode(t *testing.T) {
	post, source, facts := contentReviewInput()
	post["content"] = "Giờ cao điểm tăng 30% tốc độ, giảm 45% đơn sai, phục vụ 200 khách mỗi ngày."
	got := reviewContentReply(`{"ket_luan":"DAT","diem":9,"loi":[]}`, post, source, facts)
	defects := got["loi"].([]map[string]any)
	if len(defects) != 1 || defects[0]["loai"] != "SU_THAT" || defects[0]["sua_duoc"] != true {
		t.Fatalf("expected one fixable SU_THAT defect, got %v", defects)
	}
	detail := defects[0]["chi_tiet"].(string)
	if !strings.Contains(detail, "45%") || !strings.Contains(detail, "200") || strings.Contains(detail, "30%") {
		t.Fatalf("only the unsourced numbers are named, got %q", detail)
	}
	if got["diem"].(int) >= imageReviewPassScore {
		t.Fatalf("an invented number must pull the post under the bar, got %v", got["diem"])
	}
}

func TestMissingInformationIsNotSentBackForRewriting(t *testing.T) {
	post, source, facts := contentReviewInput()
	got := reviewContentReply(`{"ket_luan":"CANH_BAO","diem":5,"loi":[{"loai":"THIEU_Y","trich_doan":"","chi_tiet":"tiêu đề hứa số liệu tốc độ nhưng nguồn không có","sua_duoc":false}]}`, post, source, facts)
	if contentReviewNeedsFix(got) {
		t.Fatal("information the research could not find cannot be written in — notify only")
	}
}

func TestFixableContentDefectTriggersARevision(t *testing.T) {
	post, source, facts := contentReviewInput()
	got := reviewContentReply(`{"ket_luan":"CANH_BAO","diem":5,"loi":[{"loai":"CHUNG_CHUNG","trich_doan":"Không gian ấm cúng","chi_tiet":"câu đặt vào page nào cũng đúng"}]}`, post, source, facts)
	if !contentReviewNeedsFix(got) {
		t.Fatal("a generic sentence under 7 is fixable")
	}
}

func TestUnreadableContentReviewFailsClosedWithoutLooping(t *testing.T) {
	post, source, facts := contentReviewInput()
	got := reviewContentReply("Bài ổn.", post, source, facts)
	if got["ket_luan"] != reviewVerdictWarn || contentReviewNeedsFix(got) {
		t.Fatalf("an unreadable review warns and does not rewrite, got %v", got)
	}
}

// batchesOf trả lần lượt các batch (mỗi batch một bài) cho current().
func batchesOf(contents ...string) (func() map[string]any, func(string), *[]string) {
	index := 0
	var notes []string
	current := func() map[string]any {
		return map[string]any{"posts": []map[string]any{{"title": "T", "content": contents[index]}}}
	}
	revise := func(note string) {
		notes = append(notes, note)
		if index < len(contents)-1 {
			index++
		}
	}
	return current, revise, &notes
}

func scoreByContent(scores map[string]int) func(map[string]any) map[string]any {
	return func(post map[string]any) map[string]any {
		score := scores[stringArg(post, "content")]
		defects := []map[string]any{}
		verdict := reviewVerdictPass
		if score < imageReviewPassScore {
			verdict = reviewVerdictWarn
			defects = append(defects, map[string]any{"loai": "CHUNG_CHUNG", "trich_doan": stringArg(post, "content"), "chi_tiet": "chung chung", "sua_duoc": true})
		}
		return map[string]any{"ket_luan": verdict, "diem": score, "loi": defects}
	}
}

func TestContentRoundsKeepTheBestVersionOfEachPost(t *testing.T) {
	current, revise, notes := batchesOf("v1", "v2", "v3")
	batch := reviewDraftRounds(2, current, scoreByContent(map[string]int{"v1": 6, "v2": 4, "v3": 5}), revise)
	post := batch["posts"].([]map[string]any)[0]
	if post["content"] != "v1" {
		t.Fatalf("the 6/10 first draft must win, got %v", post["content"])
	}
	review := post["content_review"].(map[string]any)
	if review["diem"] != 6 || review["vong_sua"] != 2 {
		t.Fatalf("review must carry the best score and the rounds run, got %v", review)
	}
	if len(*notes) != 2 || !strings.Contains((*notes)[0], "chung chung") {
		t.Fatalf("each revision carries the reviewer's notes, got %q", *notes)
	}
}

func TestContentRoundsStopOnceEveryPostPasses(t *testing.T) {
	current, revise, notes := batchesOf("v1", "v2")
	batch := reviewDraftRounds(2, current, scoreByContent(map[string]int{"v1": 5, "v2": 8}), revise)
	post := batch["posts"].([]map[string]any)[0]
	if post["content"] != "v2" || post["content_review"].(map[string]any)["vong_sua"] != 1 || len(*notes) != 1 {
		t.Fatalf("expected v2 after one revision, got %v notes=%d", post, len(*notes))
	}
}

func TestGoodFirstDraftIsNotRevised(t *testing.T) {
	current, revise, notes := batchesOf("v1")
	batch := reviewDraftRounds(2, current, scoreByContent(map[string]int{"v1": 8}), revise)
	if len(*notes) != 0 || batch["posts"].([]map[string]any)[0]["content_review"].(map[string]any)["vong_sua"] != 0 {
		t.Fatalf("a passing draft is never rewritten, notes=%v", *notes)
	}
}

func TestDraftJobsGetRoomForReviewRounds(t *testing.T) {
	if jobRunTimeout(TekshotJobTypeDraftPosts) < autoImageRunTimeout {
		t.Fatalf("draft_posts with review rounds needs the longer timeout, got %v", jobRunTimeout(TekshotJobTypeDraftPosts))
	}
}
