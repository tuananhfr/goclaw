package tekshot

import (
	"fmt"
	"strings"
)

// Trần cứng: job GoClaw hết hạn ở 12 phút và mỗi ảnh tốn khoảng 40 giây.
const blogImagePlanMax = 6

func blogImagePlanParameters() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "Images to draw for this article: one featured image plus one per section that genuinely benefits. Leave empty only when pictures would add nothing.",
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
