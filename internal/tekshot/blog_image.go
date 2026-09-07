package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Trần cứng: job GoClaw hết hạn ở 12 phút và mỗi ảnh tốn khoảng 40 giây.
const blogImagePlanMax = 6

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
	if len(list) > blogImagePlanMax {
		return nil, fmt.Errorf("image_plan has %d entries, at most %d are allowed", len(list), blogImagePlanMax)
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
		s.setProgress(ctx, job, fmt.Sprintf("Đang tạo ảnh %d/%d", i+1, len(plan)))
		var media any
		for attempt := 1; attempt <= 2 && media == nil; attempt++ {
			result, err := loop.Run(runCtx, agent.RunRequest{
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
			if err != nil || result == nil {
				slog.Warn("tekshot: blog image attempt failed",
					"job", job.ID.String(), "target", stringFromMap(entry, "target"), "attempt", attempt)
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
