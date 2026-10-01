package tekshot

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const studioImageMaxImages = 4

// Sizes SizeFromAspect produces, plus "auto"; anything else falls back to auto.
var studioImageSizes = map[string]bool{
	"auto": true, "1024x1024": true, "1024x1360": true, "1360x1024": true, "1024x1792": true, "1792x1024": true,
}

type studioImageMedia struct {
	Path string
	Role string // "base" | "reference"
}

type studioImageRequest struct {
	Instructions string
	Prompt       string
	Size         string
	Media        []studioImageMedia
	TaggedSkills []string
	Library      []referenceLibraryItem
}

type loadedSkill struct {
	Name    string
	Content string
}

func parseStudioImageRequest(request map[string]any) (studioImageRequest, error) {
	req := studioImageRequest{
		Instructions: strings.TrimSpace(stringFromMap(request, "instructions")),
		Prompt:       strings.TrimSpace(stringFromMap(request, "prompt")),
		Size:         strings.TrimSpace(stringFromMap(request, "size")),
		Library:      referenceLibraryFromRequest(request),
	}
	if req.Prompt == "" {
		req.Prompt = strings.TrimSpace(stringFromMap(request, "message"))
	}
	if req.Prompt == "" {
		return req, fmt.Errorf("studio_image: prompt is empty")
	}
	if !studioImageSizes[req.Size] {
		req.Size = "auto"
	}
	if raw, ok := request["media"].([]any); ok {
		for _, entry := range raw {
			record, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			path := strings.TrimSpace(stringFromMap(record, "path"))
			if path == "" {
				continue
			}
			role := "reference"
			if stringFromMap(record, "role") == "base" {
				role = "base"
			}
			req.Media = append(req.Media, studioImageMedia{Path: path, Role: role})
		}
	}
	sort.SliceStable(req.Media, func(i, j int) bool {
		return req.Media[i].Role == "base" && req.Media[j].Role != "base"
	})
	seen := map[string]bool{}
	if raw, ok := request["tagged_skills"].([]any); ok {
		for _, entry := range raw {
			name, _ := entry.(string)
			name = strings.TrimSpace(name)
			if name == "" || seen[name] || len(req.TaggedSkills) >= 20 {
				continue
			}
			seen[name] = true
			req.TaggedSkills = append(req.TaggedSkills, name)
		}
	}
	return req, nil
}

func appendSkillsBlock(prompt string, skills []loadedSkill) string {
	if len(skills) == 0 {
		return prompt
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\nKỹ năng người dùng gắn:")
	for _, skill := range skills {
		sb.WriteString("\n### " + skill.Name + "\n" + strings.TrimSpace(skill.Content))
	}
	return sb.String()
}

func appendLibraryLine(prompt string, item referenceLibraryItem) string {
	return prompt + "\n\nẢnh kho (đính kèm cuối): " + headRunes(item.Description, 160)
}

// resolveStudioMediaPath only lets through files Drupal uploaded (temp dir) or
// files inside the GoClaw workspace: request paths come from outside.
func resolveStudioMediaPath(path string, roots []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("studio_image: media path must be absolute: %q", path)
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("studio_image: media path outside allowed directories: %q", path)
}
