package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const learnStyleMaxIterations = 4

// learnStyleDefaultMaxChars only applies when Drupal sends no limit; Drupal
// owns the real number because it is the side that injects the guide into
// every draft and chat prompt.
const learnStyleDefaultMaxChars = 5000

// learnStyleMaxChars reads the guide length limit off the request.
func learnStyleMaxChars(request map[string]any) int {
	if limit := int(numberFromMap(request, "style_guide_max_chars")); limit > 0 {
		return limit
	}
	return learnStyleDefaultMaxChars
}

// learnStyleMaxShortenPasses bounds the extra turns spent pulling an
// over-long guide back under the limit.
const learnStyleMaxShortenPasses = 2

// learnStyleTargetChars aims below the limit because the model overshoots the
// number it is given.
func learnStyleTargetChars(maxChars int) int {
	return maxChars * 4 / 5
}

// styleGuideLength counts characters, not bytes, to agree with Drupal's
// mb_strlen on Vietnamese text.
func styleGuideLength(guide string) int {
	return utf8.RuneCountInString(guide)
}

// shorterGuide keeps the shortest non-empty version seen so far.
func shorterGuide(current, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" && styleGuideLength(candidate) < styleGuideLength(current) {
		return candidate
	}
	return current
}

// learnStyleToolAllow keeps the style run closed-book: the job DISTILLS the
// sample posts it was handed. Letting it browse or search would invite brand
// "facts" the samples do not support. datetime stays so date references in
// samples can be reasoned about, mirroring tekshotChecklistToolAllow.
func learnStyleToolAllow() []string {
	return []string{"datetime"}
}

// learnStyleSample is one sample post from the Drupal request. Drupal
// normalises both sources (recent published posts and hand-pasted text) into
// this same sample_posts shape before enqueueing, so this is the only shape
// the runner has to parse.
type learnStyleSample struct {
	Title    string
	Content  string
	Hashtags string
}

func learnStyleSamples(request map[string]any) []learnStyleSample {
	raw, ok := request["sample_posts"].([]any)
	if !ok {
		return nil
	}
	samples := make([]learnStyleSample, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(stringFromMap(entry, "content"))
		if content == "" {
			continue
		}
		samples = append(samples, learnStyleSample{
			Title:    strings.TrimSpace(stringFromMap(entry, "title")),
			Content:  content,
			Hashtags: strings.TrimSpace(stringFromMap(entry, "hashtags")),
		})
	}
	return samples
}

func (s *JobService) runLearnStyle(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("goclaw chưa cấu hình agent router")
	}
	samples := learnStyleSamples(request)
	if len(samples) == 0 {
		return nil, "", fmt.Errorf("cần ít nhất một bài mẫu có nội dung để học style")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	runReq := agent.RunRequest{
		SessionKey:    job.SessionKey,
		Message:       buildLearnStylePrompt(request, samples),
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     learnStyleToolAllow(),
		MaxIterations: learnStyleMaxIterations,
		TraceName:     "tekshot learn style",
		TraceTags:     []string{"tekshot", "learn_style"},
	}

	result, err := loop.Run(runCtx, runReq)
	if err != nil {
		return nil, "", err
	}
	if result == nil {
		return nil, "", fmt.Errorf("agent không trả kết quả khi học style")
	}
	styleGuide := strings.TrimSpace(result.Content)
	if styleGuide == "" {
		return nil, "", fmt.Errorf("agent trả về style guide rỗng, style guide cũ được giữ nguyên")
	}

	// Giới hạn là mục tiêu chứ không phải cổng chặn: fail ở đây thì một page có
	// style guide dài sẽ không bao giờ học được nữa, nên vẫn lưu bản ngắn nhất.
	maxChars := learnStyleMaxChars(request)
	for pass := 1; pass <= learnStyleMaxShortenPasses && styleGuideLength(styleGuide) > maxChars; pass++ {
		shortenReq := runReq
		shortenReq.SessionKey = fmt.Sprintf("%s:shorten:%d", job.SessionKey, pass)
		shortenReq.RunID = uuid.NewString()
		shortenReq.Message = buildLearnStyleShortenPrompt(styleGuide, styleGuideLength(styleGuide), maxChars)
		shortenReq.MaxIterations = 1
		shortened, err := loop.Run(runCtx, shortenReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", err
			}
			slog.Warn("tekshot.learn_style.shorten_failed", "job_id", job.ID, "pass", pass, "error", err)
			break
		}
		if shortened == nil {
			break
		}
		next := shorterGuide(styleGuide, shortened.Content)
		if next == styleGuide {
			break
		}
		styleGuide = next
	}
	if length := styleGuideLength(styleGuide); length > maxChars {
		slog.Warn("tekshot.learn_style.over_limit", "job_id", job.ID, "chars", length, "limit", maxChars)
	}
	return map[string]any{"style_guide": styleGuide}, "Style guide generated", nil
}

// buildLearnStyleShortenPrompt is self-contained so the shorten pass does not
// depend on the first run's session history.
func buildLearnStyleShortenPrompt(guide string, length, maxChars int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("The style guide below is %d characters, over the %d-character limit.\n", length, maxChars))
	sb.WriteString(fmt.Sprintf("Rewrite it to about %d characters, never more than %d.\n\n", learnStyleTargetChars(maxChars), maxChars))
	sb.WriteString("## Hard rules\n")
	sb.WriteString("- Giữ nguyên cấu trúc và tên các mục; gộp ý trùng, bỏ ví dụ dài, viết gạch đầu dòng ngắn.\n")
	sb.WriteString("- Giữ đúng 1 bài mẫu tham chiếu nguyên văn ở cuối.\n")
	sb.WriteString("- Reply with ONLY the style guide text: no preamble, no meta commentary, no code fences.\n\n")
	sb.WriteString("## Style guide to shorten\n")
	sb.WriteString(guide)
	sb.WriteString("\n")
	return sb.String()
}

func buildLearnStylePrompt(request map[string]any, samples []learnStyleSample) string {
	pageName := strings.TrimSpace(stringFromMap(request, "facebook_page_name"))
	pageDesc := strings.TrimSpace(stringFromMap(request, "facebook_page_description"))
	styleSource := strings.TrimSpace(stringFromMap(request, "style_source"))
	currentGuide := strings.TrimSpace(stringFromMap(request, "current_style_guide"))
	maxChars := learnStyleMaxChars(request)
	updating := currentGuide != ""

	var sb strings.Builder
	if updating {
		// Cập nhật chứ không viết lại: bài cũ trôi khỏi tập mẫu thì những gì học
		// từ chúng vẫn phải còn trong style guide.
		sb.WriteString("You are updating an existing writing style guide for a Facebook page.\n")
		sb.WriteString("The current guide is the base. Use the new sample posts to refine it — do not rewrite it from scratch.\n\n")
	} else {
		sb.WriteString("You are analysing a Facebook page's posts to distill the page's writing style.\n")
		sb.WriteString("Study the sample posts below and produce a practical style guide that a copywriter (or another AI) can follow to write NEW posts in exactly the same voice.\n\n")
	}

	sb.WriteString("## Page\n")
	if pageName != "" {
		sb.WriteString("- Name: " + pageName + "\n")
	}
	if pageDesc != "" {
		sb.WriteString("- Description: " + pageDesc + "\n")
	}
	if styleSource != "" {
		sb.WriteString("- Sample source: " + styleSource + "\n")
	}

	if updating {
		sb.WriteString("\n## Current style guide\n")
		sb.WriteString(currentGuide + "\n")
		sb.WriteString(fmt.Sprintf("\n## New sample posts (%d)\n", len(samples)))
	} else {
		sb.WriteString(fmt.Sprintf("\n## Sample posts (%d)\n", len(samples)))
	}
	for i, sample := range samples {
		sb.WriteString(fmt.Sprintf("\n### Sample %d\n", i+1))
		if sample.Title != "" {
			sb.WriteString("Title: " + sample.Title + "\n")
		}
		sb.WriteString("Content:\n" + sample.Content + "\n")
		if sample.Hashtags != "" {
			sb.WriteString("Hashtags: " + sample.Hashtags + "\n")
		}
	}

	sb.WriteString("\n## Hard rules\n")
	sb.WriteString("- Write the ENTIRE style guide in Vietnamese — store staff read and edit it by hand.\n")
	// Bài mẫu nguyên vẹn dạy giọng văn tốt hơn mô tả trừu tượng: người viết sau
	// (thường là một agent khác) học nhịp câu và cách triển khai ý từ bài thật.
	sb.WriteString("- Cover, in this order: giọng văn và thái độ; cách xưng hô với khách; cấu trúc bài (mở - thân - kết); cách dùng emoji (mật độ, vị trí, bộ emoji quen thuộc); độ dài điển hình; phong cách hashtag; kiểu call-to-action đặc trưng.\n")
	sb.WriteString("- Kết thúc bằng mục \"Bài mẫu tham chiếu\" chứa đúng 1 bài ĐẦY ĐỦ nguyên văn (title + content + hashtags) tiêu biểu nhất cho giọng của page. Ghi rõ đây là mẫu để học nhịp và giọng, không phải câu chữ để copy.\n")
	if updating {
		sb.WriteString("- Giữ nguyên cấu trúc và tên các mục của style guide hiện tại.\n")
		sb.WriteString("- Giữ mọi quan sát mà bài mới không mâu thuẫn; không xoá một mục chỉ vì bài mới không nhắc tới nó.\n")
		sb.WriteString("- Bổ sung đặc điểm mới khi bài mới cho thấy rõ; chỉ sửa một quan sát khi bài mới cho thấy rõ page đã đổi cách viết.\n")
		sb.WriteString("- Ít bài mẫu thì thay đổi ít: một bài lệch giọng không đủ để đổi luật.\n")
		sb.WriteString("- Giữ bài mẫu tham chiếu hiện có, trừ khi một bài mới tiêu biểu hơn rõ rệt.\n")
		if styleGuideLength(currentGuide) > maxChars {
			sb.WriteString("- Style guide hiện tại đang dài hơn giới hạn: rút gọn nó, giữ các quan sát quan trọng nhất.\n")
		}
	}
	sb.WriteString(fmt.Sprintf("- Viết cả style guide, kể cả bài mẫu tham chiếu, khoảng %d ký tự và tuyệt đối tối đa %d ký tự: gạch đầu dòng ngắn, không lặp ý, không liệt kê ví dụ dài.\n", learnStyleTargetChars(maxChars), maxChars))
	sb.WriteString("- Ground EVERY observation in the samples above. Never invent products, prices, promotions or brand facts the samples do not show.\n")
	sb.WriteString("- Reply with ONLY the style guide text: no preamble, no meta commentary, no code fences.\n")
	return sb.String()
}
