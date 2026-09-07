package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Trần cứng: job GoClaw hết hạn ở 12 phút và mỗi ảnh tốn khoảng 40 giây.
const (
	blogImagePlanMax      = 6
	blogImagePlanMin      = 2
	blogImagePlanToolName = "submit_blog_image_plan"
	blogImagePlanNoTools  = "blog-image-plan/no-tools"
	// Vòng 2 là lần nộp lại khi vòng 1 bị từ chối. Nó thường phí một lượt nộp
	// y hệt, nhưng blogImagePlanTimeout chặn đứng lượt đó — bỏ nó đi thì một
	// plan sai là cả bài không có ảnh nào.
	blogImagePlanIterations = 2
	// Trần riêng để lượt này không bao giờ tiêu vào ngân sách vẽ ảnh.
	blogImagePlanTimeout = 90 * time.Second
	// Mỗi ảnh ~45s; hết ngân sách thì dừng, đừng vẽ vào một context đã chết.
	blogImageDrawTimeout = 120 * time.Second
)

func blogImagePlanParameters() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "Images to draw for this article. Always include one \"featured\" cover, and one image for each section a reader could picture: 3 to 5 entries is the norm, 6 the maximum. An empty list is only for a pure reference table with nothing to illustrate, and is almost never the right answer.",
		"items": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"target":  map[string]any{"type": "string", "description": "\"featured\" for the article's cover, or \"section:<id>\" for one section. One image per target at most."},
				"prompt":  map[string]any{"type": "string", "description": "What to draw, in English, 20-60 words: subject, setting, framing, lighting, mood. Photographic and realistic unless the article is abstract. No text or logos in the picture."},
				"alt":     map[string]any{"type": "string", "description": "Alt text in the article's language, describing the picture for a reader who cannot see it."},
				"caption": map[string]any{"type": "string", "description": "Optional caption shown under the image; empty string for none."},
			},
			"required": []string{"target", "prompt", "alt", "caption"},
		},
	}
}

// validateBlogImagePlan chuẩn hoá plan và gắn media=nil. Target lạ bị từ chối
// ngay tại đây thay vì vẽ xong mới phát hiện không gắn được vào bài.
func validateBlogImagePlan(raw any, sectionIDs map[string]bool) ([]any, error) {
	if raw == nil {
		return []any{}, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("image_plan must be an array")
	}
	// Thừa ảnh thì cắt, đừng từ chối: trần này là ngân sách thời gian, và một
	// lượt bị từ chối là cả bài không có ảnh nào.
	if len(list) > blogImagePlanMax {
		list = list[:blogImagePlanMax]
	}
	seen := map[string]bool{}
	out := make([]any, 0, len(list))
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("image_plan[%d] must be an object", i)
		}
		target := strings.TrimSpace(stringFromMap(entry, "target"))
		if target != "featured" {
			id, found := strings.CutPrefix(target, blogScopeSectionPrfx)
			if !found || !sectionIDs[id] {
				return nil, fmt.Errorf("image_plan[%d].target %q must be \"featured\" or \"section:<id>\" of a section in the document", i, target)
			}
		}
		if seen[target] {
			return nil, fmt.Errorf("image_plan has two entries for %q", target)
		}
		seen[target] = true
		prompt := strings.TrimSpace(stringFromMap(entry, "prompt"))
		if prompt == "" {
			return nil, fmt.Errorf("image_plan[%d].prompt must not be empty", i)
		}
		alt := strings.TrimSpace(stringFromMap(entry, "alt"))
		if alt == "" {
			return nil, fmt.Errorf("image_plan[%d].alt must not be empty", i)
		}
		out = append(out, map[string]any{
			"target":  target,
			"prompt":  cutRunes(prompt, 1200),
			"alt":     cutRunes(alt, 512),
			"caption": cutRunes(strings.TrimSpace(stringFromMap(entry, "caption")), 512),
			"media":   nil,
		})
	}
	return out, nil
}

// BlogImagePlanCollector nhận kế hoạch ảnh. Plan quá mỏng bị trả lỗi để model
// nghĩ lại, nhưng vẫn được giữ lại: thà một ảnh còn hơn không ảnh nào.
type BlogImagePlanCollector struct {
	sectionIDs map[string]bool
	plan       []any
}

func NewBlogImagePlanCollector(sectionIDs map[string]bool) *BlogImagePlanCollector {
	return &BlogImagePlanCollector{sectionIDs: sectionIDs}
}

func (t *BlogImagePlanCollector) Name() string { return blogImagePlanToolName }

func (t *BlogImagePlanCollector) Description() string {
	return "Submit the list of images to draw for the article you just wrote: one cover plus one image for each section a reader could picture. Call exactly once."
}

func (t *BlogImagePlanCollector) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"image_plan": blogImagePlanParameters()},
		"required":             []string{"image_plan"},
	}
}

func (t *BlogImagePlanCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	plan, err := validateBlogImagePlan(args["image_plan"], t.sectionIDs)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.plan = plan
	if len(plan) < blogImagePlanMin {
		return tools.ErrorResult(fmt.Sprintf(
			"MODEL_OUTPUT_INVALID: image_plan has %d entry; this article is illustrated, so plan a \"featured\" cover plus one image per section a reader could picture (3 to 5 entries is the norm). Call %s again with the full list.",
			len(plan), blogImagePlanToolName))
	}
	return tools.SilentResult("Image plan captured.")
}

func (t *BlogImagePlanCollector) Report() []any { return t.plan }

// planBlogImages là một lượt riêng sau khi bài đã xong. Gộp vào cùng lệnh nộp
// bài thì model viết xong 11KB rồi lên cho có đúng một ảnh.
func (s *JobService) planBlogImages(ctx context.Context, job *store.TekshotJob, loop agent.Agent, document map[string]any) []any {
	collector := NewBlogImagePlanCollector(blogSectionIDs(document))
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)
	runCtx, cancel := context.WithTimeout(runCtx, blogImagePlanTimeout)
	defer cancel()

	if _, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:     job.SessionKey + ":image-plan",
		Message:        buildBlogImagePlanPrompt(document),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      []string{blogImagePlanNoTools},
		EphemeralTools: []tools.Tool{collector},
		ToolChoice:     &providers.ToolChoice{Mode: "function", Name: blogImagePlanToolName},
		MaxIterations:  blogImagePlanIterations,
		SkillFilter:    []string{},
		LightContext:   true,
		HistoryLimit:   1,
		TraceName:      "tekshot blog image plan",
		TraceTags:      []string{"tekshot", "blog", "image"},
	}); err != nil && collector.Report() == nil {
		slog.Warn("tekshot: blog image plan failed", "job", job.ID.String(), "error", err)
	}
	return collector.Report()
}

// buildBlogImagePlanPrompt chỉ đưa khung bài, không đưa cả nội dung: model vừa
// viết xong nên chỉ cần nhắc lại nó đang minh hoạ cho cái gì.
func buildBlogImagePlanPrompt(document map[string]any) string {
	var sb strings.Builder
	sb.WriteString("[System] You just finished writing this article. Now plan its pictures by calling " + blogImagePlanToolName + " exactly once. Do not reply with plain text.\n")
	sb.WriteString("Plan one \"featured\" cover, plus one image for every section below that a reader could picture — 3 to 5 entries is the norm, 6 is the ceiling. Only a pure reference table with nothing to illustrate may be left out.\n")
	sb.WriteString("Each entry: an English drawing prompt of 20-60 words (subject, setting, framing, lighting, mood) and an alt text in the article's language. No text, no logo and no watermark in the picture.\n\n")
	sb.WriteString("TITLE: " + stringFromMap(document, "title") + "\n")
	if summary := stringFromMap(document, "summary"); summary != "" {
		sb.WriteString("SUMMARY: " + summary + "\n")
	}
	sb.WriteString("SECTIONS:\n")
	sections, _ := document["sections"].([]any)
	for _, raw := range sections {
		section, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		sb.WriteString("- section:" + stringFromMap(section, "id") + " — " + stringFromMap(section, "heading") + "\n")
	}
	return sb.String()
}

// generateBlogImages vẽ từng ảnh trong plan bằng một lượt create_image bắt
// buộc. MaxIterations phải là 1: ToolChoice được áp lại mỗi vòng, N vòng là N
// ảnh. Một ảnh hỏng không kéo cả bài — mục đó giữ media nil và Drupal trỏ vào
// ảnh tạm của site.
func (s *JobService) generateBlogImages(ctx context.Context, job *store.TekshotJob, loop agent.Agent, plan []any) []any {
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	for i, item := range plan {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// Ngân sách job đã hết thì mọi lượt sau chỉ fail tức thì trong vài ms —
		// dừng hẳn để phần còn lại đi thẳng vào ảnh tạm.
		if runCtx.Err() != nil {
			slog.Warn("tekshot: blog images stopped, job budget spent",
				"job", job.ID.String(), "drawn", i, "planned", len(plan), "error", runCtx.Err())
			break
		}
		s.setProgress(ctx, job, fmt.Sprintf("Đang tạo ảnh %d/%d", i+1, len(plan)))
		var media any
		for attempt := 1; attempt <= 2 && media == nil; attempt++ {
			drawCtx, cancelDraw := context.WithTimeout(runCtx, blogImageDrawTimeout)
			result, err := loop.Run(drawCtx, agent.RunRequest{
				SessionKey:    job.SessionKey + ":image:" + strconv.Itoa(i),
				Message:       buildBlogImagePrompt(entry),
				Channel:       "tekshot_job",
				ChannelType:   "tekshot",
				ChatID:        userID,
				PeerKind:      "direct",
				Addressed:     true,
				RunID:         uuid.NewString(),
				UserID:        userID,
				SenderID:      userID,
				MaxIterations: 1,
				ToolChoice:    &providers.ToolChoice{Mode: "function", Name: "create_image"},
				LightContext:  true,
				HistoryLimit:  1,
				TraceName:     "tekshot blog image",
				TraceTags:     []string{"tekshot", "blog", "image"},
			})
			cancelDraw()
			if err != nil || result == nil {
				slog.Warn("tekshot: blog image attempt failed",
					"job", job.ID.String(), "target", stringFromMap(entry, "target"), "attempt", attempt, "error", err)
				continue
			}
			media = blogImageMediaEntry(result.Media)
		}
		if media == nil {
			slog.Warn("tekshot: blog image gave up",
				"job", job.ID.String(), "target", stringFromMap(entry, "target"))
		}
		entry["media"] = media
		plan[i] = entry
	}
	return plan
}

// blogImageMediaEntry lấy ảnh cuối cùng: create_image trả một ảnh, nhiều hơn
// nghĩa là lượt bắt buộc đã lệch và ảnh sau là ảnh gần yêu cầu nhất.
func blogImageMediaEntry(media []agent.MediaResult) any {
	if len(media) == 0 {
		return nil
	}
	last := media[len(media)-1]
	// MediaResult không mang tên file; workspace path luôn dùng dấu gạch xuôi.
	return map[string]any{
		"path":      last.Path,
		"mime_type": last.ContentType,
		"filename":  path.Base(last.Path),
	}
}

const blogImagePromptHead = `[System] Call create_image now — do not reply with plain text. Draw exactly one image for a blog article.
Subject: `

const blogImagePromptTail = `
Photographic and realistic unless the subject is abstract. Natural light, believable setting, no staged stock-photo poses.
No text, no caption, no watermark, no logo and no brand mark anywhere in the picture — text drawn into an image cannot be read by a search engine and cannot be translated.
Leave reference_image_path empty. Use aspect_ratio 16:9.
`

func buildBlogImagePrompt(entry map[string]any) string {
	return blogImagePromptHead + stringFromMap(entry, "prompt") + blogImagePromptTail
}
