package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func resolveUploadedFileByBasename(ctx context.Context, workspace, name string) (string, bool, error) {
	name = filepath.Base(normalizeMediaPath(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", false, nil
	}

	var matches []string
	seen := make(map[string]bool)
	for _, root := range uploadSearchRoots(ctx, workspace) {
		for _, candidate := range uploadCandidates(root, name) {
			clean := filepath.Clean(candidate)
			if seen[clean] {
				continue
			}
			if info, err := os.Stat(clean); err == nil && info.Mode().IsRegular() {
				seen[clean] = true
				matches = append(matches, clean)
			}
		}
	}

	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		return matches[0], true, nil
	default:
		return "", false, fmt.Errorf("multiple uploaded files named %q were found; use the full path", name)
	}
}

func uploadSearchRoots(ctx context.Context, workspace string) []string {
	roots := []string{
		workspace,
		ToolTeamWorkspaceFromCtx(ctx),
		ToolTeamGlobalWorkspaceFromCtx(ctx),
		ToolTeamRootFromCtx(ctx),
	}

	var out []string
	seen := make(map[string]bool)
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
		parent := uploadSiblingRoot(root)
		if parent != "" && !seen[parent] {
			seen[parent] = true
			out = append(out, parent)
		}
	}
	return out
}

func uploadSiblingRoot(root string) string {
	parent := filepath.Dir(root)
	if parent == "." || parent == root {
		return ""
	}
	// Avoid widening from a team root to the all-teams directory.
	if filepath.Base(parent) == "teams" {
		return ""
	}
	return parent
}

func uploadCandidates(root, name string) []string {
	candidates := []string{filepath.Join(root, ".uploads", name)}
	entries, err := os.ReadDir(root)
	if err != nil {
		return candidates
	}
	for _, entry := range entries {
		if entry.IsDir() {
			candidates = append(candidates, filepath.Join(root, entry.Name(), ".uploads", name))
		}
	}
	return candidates
}
