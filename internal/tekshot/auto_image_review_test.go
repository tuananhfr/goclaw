package tekshot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// scripted trả lần lượt các điểm cho từng ảnh; ảnh thứ i mang path "img-i".
func scripted(t *testing.T, scores []int, fixable bool) (func(string) (map[string]any, error), func(map[string]any) map[string]any, *[]string) {
	t.Helper()
	var notes []string
	calls := 0
	generate := func(note string) (map[string]any, error) {
		notes = append(notes, note)
		calls++
		return map[string]any{"media": []any{map[string]any{"path": "img-" + string(rune('0'+calls))}}}, nil
	}
	review := func(result map[string]any) map[string]any {
		index := int(firstMediaPath(result)[4] - '1')
		score := scores[index]
		verdict := reviewVerdictPass
		defects := []map[string]any{}
		if score < imageReviewPassScore {
			verdict = reviewVerdictWarn
			defects = append(defects, map[string]any{"loai": "THONG_DIEP", "vi_tri": "toàn ảnh", "chi_tiet": "lỗi ở ảnh " + firstMediaPath(result), "sua_duoc": fixable})
		}
		return map[string]any{"ket_luan": verdict, "diem": score, "loi": defects}
	}
	return generate, review, &notes
}

func TestGoodFirstImageIsKeptWithoutFixing(t *testing.T) {
	generate, review, notes := scripted(t, []int{8}, true)
	best, bestReview, rounds, err := runReviewedRounds(2, generate, review)
	if err != nil || firstMediaPath(best) != "img-1" || rounds != 0 || reviewScore(bestReview) != 8 {
		t.Fatalf("got best=%v rounds=%d review=%v err=%v", best, rounds, bestReview, err)
	}
	if len(*notes) != 1 || (*notes)[0] != "" {
		t.Fatalf("the first attempt carries no review notes, got %q", *notes)
	}
}

func TestFixRoundsStopOnceTheBarIsMet(t *testing.T) {
	generate, review, notes := scripted(t, []int{5, 6, 8}, true)
	best, bestReview, rounds, _ := runReviewedRounds(2, generate, review)
	if firstMediaPath(best) != "img-3" || rounds != 2 || reviewScore(bestReview) != 8 {
		t.Fatalf("expected the third image after two fixes, got %v rounds=%d", best, rounds)
	}
	if !strings.Contains((*notes)[1], "lỗi ở ảnh img-1") || !strings.Contains((*notes)[2], "lỗi ở ảnh img-2") {
		t.Fatalf("each fix must carry the previous review, got %q", *notes)
	}
}

func TestBestImageIsKeptWhenFixesMakeItWorse(t *testing.T) {
	generate, review, _ := scripted(t, []int{6, 4, 5}, true)
	best, bestReview, rounds, _ := runReviewedRounds(2, generate, review)
	if firstMediaPath(best) != "img-1" || reviewScore(bestReview) != 6 || rounds != 2 {
		t.Fatalf("the 6/10 first image must win, got %v score=%d rounds=%d", best, reviewScore(bestReview), rounds)
	}
}

func TestUnfixableDefectsNeverTriggerAFix(t *testing.T) {
	generate, review, notes := scripted(t, []int{4, 9}, false)
	_, _, rounds, _ := runReviewedRounds(2, generate, review)
	if rounds != 0 || len(*notes) != 1 {
		t.Fatalf("a real-photo requirement cannot be regenerated away, rounds=%d", rounds)
	}
}

func TestFailedGenerationUsesARoundAndCarriesTheError(t *testing.T) {
	calls := 0
	var notes []string
	generate := func(note string) (map[string]any, error) {
		notes = append(notes, note)
		calls++
		if calls == 1 {
			return nil, errors.New("codex native image: context deadline exceeded")
		}
		return map[string]any{"media": []any{map[string]any{"path": "img-2"}}}, nil
	}
	review := func(map[string]any) map[string]any {
		return map[string]any{"ket_luan": reviewVerdictPass, "diem": 8, "loi": []map[string]any{}}
	}
	best, _, rounds, err := runReviewedRounds(2, generate, review)
	if err != nil || firstMediaPath(best) != "img-2" || rounds != 1 {
		t.Fatalf("got best=%v rounds=%d err=%v", best, rounds, err)
	}
	if !strings.Contains(notes[1], "context deadline exceeded") {
		t.Fatalf("the retry must know why the last attempt failed, got %q", notes[1])
	}
}

func TestNoImageAtAllIsAnError(t *testing.T) {
	generate := func(string) (map[string]any, error) { return map[string]any{"media": []any{}}, nil }
	review := func(map[string]any) map[string]any { t.Fatal("nothing to review"); return nil }
	if _, _, _, err := runReviewedRounds(2, generate, review); err == nil {
		t.Fatal("three attempts without an image must fail the job")
	}
}

func TestReviewedImageJobsGetALongerRunTimeout(t *testing.T) {
	// 3 lượt sinh (2-5 phút mỗi lượt) + 3 lượt soát không vừa 12 phút mặc định.
	if got := jobRunTimeout(TekshotJobTypeAutoImage); got < 30*time.Minute {
		t.Fatalf("auto_image needs room for its fix rounds, got %v", got)
	}
	if got := jobRunTimeout(TekshotJobTypePostChat); got != defaultJobRunTimeout {
		t.Fatalf("other jobs keep the default, got %v", got)
	}
}
