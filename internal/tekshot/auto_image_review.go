package tekshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// autoImageFixRounds: số lượt sinh lại tối đa sau ảnh đầu (chủ page chốt 2).
	autoImageFixRounds = 2
	// 3 lượt sinh (2-5 phút mỗi lượt) + 3 lượt soát không vừa 12 phút mặc định.
	autoImageRunTimeout = 30 * time.Minute
)

// jobRunTimeout: ảnh cron và viết bài có vòng soát + sửa, không vừa 12 phút.
func jobRunTimeout(jobType string) time.Duration {
	if jobType == TekshotJobTypeAutoImage || jobType == TekshotJobTypeDraftPosts {
		return autoImageRunTimeout
	}
	return defaultJobRunTimeout
}

// runReviewedRounds sinh ảnh, cho người soát độc lập chấm, và sinh lại kèm góp
// ý khi dưới mốc mà còn lỗi sửa được. Luôn giữ ảnh điểm cao nhất — sinh lại
// không chắc tốt hơn, cùng model hay lặp đúng lỗi cũ.
func runReviewedRounds(
	maxFix int,
	generate func(notes string) (map[string]any, error),
	review func(result map[string]any) map[string]any,
) (map[string]any, map[string]any, int, error) {
	var best, bestReview map[string]any
	bestScore := -1
	notes := ""
	lastErr := ""
	rounds := 0
	for attempt := 0; attempt <= maxFix; attempt++ {
		rounds = attempt
		result, err := generate(notes)
		if err != nil {
			lastErr = err.Error()
			notes = "The previous attempt failed before producing an image: " + lastErr + "\nTry again with the same concept."
			continue
		}
		if firstMediaPath(result) == "" {
			lastErr = "no image was returned"
			notes = "The previous attempt returned no image. Call create_image this time."
			continue
		}
		verdict := review(result)
		if score := reviewScore(verdict); score > bestScore {
			best, bestReview, bestScore = result, verdict, score
		}
		if !imageReviewNeedsFix(verdict) {
			break
		}
		notes = imageReviewFixNotes(verdict)
	}
	if best == nil {
		return nil, nil, rounds, fmt.Errorf("automated image produced no usable image after %d attempts: %s", maxFix+1, lastErr)
	}
	return best, bestReview, rounds, nil
}

// runAutoImageReviewed thay vòng tự chấm của agent sinh ảnh bằng người soát
// độc lập (agent chung Drupal chọn). Tự chấm đã cho qua biển hiệu bịa chữ.
func (s *JobService) runAutoImageReviewed(ctx context.Context, job *store.TekshotJob, request map[string]any, reviewAgent string) (any, string, error) {
	basePrompt := strings.TrimSpace(stringFromMap(request, "prompt"))
	imageJob := *job
	imageJob.JobType = TekshotJobTypeImageChat

	// setProgress cũng gia hạn khoá job (10 phút): vòng sửa dài hơn thế, không
	// gia hạn thì worker khác nhận lại job đang chạy dở.
	attempt := 0
	generate := func(notes string) (map[string]any, error) {
		attempt++
		s.setProgress(ctx, job, fmt.Sprintf("Sinh ảnh lượt %d/%d", attempt, autoImageFixRounds+1))
		attemptRequest := cloneMap(request)
		prompt := basePrompt
		if notes != "" {
			prompt += "\n\nINDEPENDENT REVIEW OF THE PREVIOUS IMAGE — keep the concept unless the review says the message is wrong:\n" + notes
		}
		attemptRequest["prompt"] = automatedImagePrompt(prompt, attemptRequest)
		result, _, err := s.runChat(ctx, &imageJob, attemptRequest)
		if err != nil {
			return nil, err
		}
		resultMap, ok := result.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("image runner returned %T", result)
		}
		return resultMap, nil
	}
	review := func(result map[string]any) map[string]any {
		s.setProgress(ctx, job, fmt.Sprintf("Soát ảnh lượt %d/%d", attempt, autoImageFixRounds+1))
		return s.reviewImage(ctx, job, reviewAgent, request, []bus.MediaFile{{Path: firstMediaPath(result)}})
	}

	best, bestReview, rounds, err := runReviewedRounds(autoImageFixRounds, generate, review)
	if err != nil {
		return nil, "", err
	}
	// Agent sinh ảnh vẫn trả creative_plan (Drupal dùng làm lịch sử concept).
	plan := parseAutoImageQA(stringFromMap(best, "content"))
	best["creative_plan"] = plan.CreativePlan
	best["final_prompt"] = plan.FinalPrompt
	best["qa_passed"] = true
	best["qa_notes"] = "Independent review"
	bestReview["vong_sua"] = rounds
	best["review"] = bestReview
	return best, fmt.Sprintf("Automated image reviewed: %d/10 after %d fix round(s)", reviewScore(bestReview), rounds), nil
}
