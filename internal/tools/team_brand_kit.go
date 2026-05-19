package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

func cleanTeamMaterialsPath(args map[string]any) (string, error) {
	raw, _ := args["brand_kit"].(string)
	if raw == "" {
		raw, _ = args["materials_path"].(string)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	raw = filepath.ToSlash(raw)
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.Contains(raw, ":") {
		return "", fmt.Errorf("brand_kit/materials_path must be a relative team workspace path, e.g. brand-kits/pizza-hips")
	}
	clean := filepath.Clean(raw)
	clean = filepath.ToSlash(clean)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("brand_kit/materials_path cannot be empty")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("brand_kit/materials_path cannot escape the team workspace")
	}
	return clean, nil
}

func taskBrandKit(taskMeta map[string]any) string {
	if taskMeta == nil {
		return ""
	}
	if v, ok := taskMeta[TaskMetaBrandKit].(string); ok {
		return v
	}
	return ""
}
