package tekshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type studioImageAgentKey struct{}

type studioImageAgent struct {
	data *store.AgentData
	err  error
}

func (s *JobService) withStudioImageAgent(ctx context.Context, job *store.TekshotJob) context.Context {
	if _, ok := ctx.Value(studioImageAgentKey{}).(*studioImageAgent); ok {
		return ctx
	}
	ctx = store.WithTenantID(ctx, store.MasterTenantID)
	resolved := &studioImageAgent{}
	switch {
	case strings.TrimSpace(job.AgentKey) == "":
		resolved.err = fmt.Errorf("studio_image: agent_key is required")
	case s.studio == nil || s.studio.Agents == nil:
		resolved.err = fmt.Errorf("studio_image: agent store is not configured")
	default:
		resolved.data, resolved.err = s.studio.Agents.GetByKey(ctx, job.AgentKey)
		if resolved.err != nil {
			resolved.err = fmt.Errorf("studio_image: load agent %q: %w", job.AgentKey, resolved.err)
		} else if resolved.data == nil {
			resolved.err = fmt.Errorf("studio_image: agent %q was not found", job.AgentKey)
		}
	}
	return context.WithValue(ctx, studioImageAgentKey{}, resolved)
}

func studioImageAgentFromContext(ctx context.Context) *studioImageAgent {
	resolved, _ := ctx.Value(studioImageAgentKey{}).(*studioImageAgent)
	return resolved
}

func (s *JobService) studioImageWorkspace(ctx context.Context) (string, error) {
	resolved := studioImageAgentFromContext(ctx)
	if resolved == nil {
		return "", fmt.Errorf("studio_image: agent workspace was not resolved")
	}
	if resolved.err != nil {
		return "", resolved.err
	}
	if strings.TrimSpace(resolved.data.Workspace) == "" {
		return "", fmt.Errorf("studio_image: agent %q has no workspace", resolved.data.AgentKey)
	}
	root, err := filepath.Abs(config.ExpandHome(s.studio.Workspace))
	if err != nil {
		return "", fmt.Errorf("studio_image: resolve gateway workspace: %w", err)
	}
	target, err := filepath.Abs(config.ExpandHome(resolved.data.Workspace))
	if err != nil {
		return "", fmt.Errorf("studio_image: resolve agent workspace: %w", err)
	}
	if !studioImagePathWithin(root, target) {
		return "", fmt.Errorf("studio_image: agent workspace must be a directory inside the gateway workspace")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("studio_image: inspect gateway workspace: %w", err)
	}
	realTarget, err := studioImageResolvedPath(target)
	if err != nil {
		return "", fmt.Errorf("studio_image: inspect agent workspace: %w", err)
	}
	if !studioImagePathWithin(realRoot, realTarget) {
		return "", fmt.Errorf("studio_image: agent workspace resolves outside the gateway workspace")
	}
	// The date folder may already exist, so validate its symlinks before calling the model.
	outputDir := filepath.Join(target, "tekshot_studio", time.Now().Format("2006-01-02"))
	realOutput, err := studioImageResolvedPath(outputDir)
	if err != nil {
		return "", fmt.Errorf("studio_image: inspect image output directory: %w", err)
	}
	if !studioImagePathWithin(realTarget, realOutput) {
		return "", fmt.Errorf("studio_image: image output directory resolves outside the agent workspace")
	}
	return target, nil
}

func studioImagePathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func studioImageResolvedPath(path string) (string, error) {
	ancestor := path
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			rel, err := filepath.Rel(ancestor, path)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, rel), nil
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return "", err
		}
		ancestor = filepath.Dir(ancestor)
	}
}
