package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// ListFilesTool lists files in a directory, optionally through a sandbox container.
type ListFilesTool struct {
	workspace       string
	restrict        bool
	allowedPrefixes []string // extra allowed path prefixes (e.g. skills dirs)
	deniedPrefixes  []string // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr      sandbox.Manager
	contextFileIntc *ContextFileInterceptor // unused, satisfies InterceptorAware
	memIntc         *MemoryInterceptor      // nil = no memory routing
}

func (t *ListFilesTool) SetContextFileInterceptor(intc *ContextFileInterceptor) {
	t.contextFileIntc = intc
}

func (t *ListFilesTool) SetMemoryInterceptor(intc *MemoryInterceptor) {
	t.memIntc = intc
}

// AllowPaths adds extra path prefixes that list_files is allowed to access
// even when restrict_to_workspace is true (e.g. skills directories).
func (t *ListFilesTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}

// AllowedPaths returns a copy of configured allowed path prefixes.
func (t *ListFilesTool) AllowedPaths() []string {
	return append([]string(nil), t.allowedPrefixes...)
}

// DenyPaths adds path prefixes that list_files must reject/filter.
func (t *ListFilesTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

// DeniedPaths returns a copy of configured denied path prefixes.
func (t *ListFilesTool) DeniedPaths() []string {
	return append([]string(nil), t.deniedPrefixes...)
}

func NewListFilesTool(workspace string, restrict bool) *ListFilesTool {
	return &ListFilesTool{workspace: workspace, restrict: restrict}
}

func NewSandboxedListFilesTool(workspace string, restrict bool, mgr sandbox.Manager) *ListFilesTool {
	return &ListFilesTool{workspace: workspace, restrict: restrict, sandboxMgr: mgr}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *ListFilesTool) SetSandboxKey(key string) {}

func (t *ListFilesTool) Name() string        { return "list_files" }
func (t *ListFilesTool) Description() string { return "List files and directories in a path" }
func (t *ListFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path (relative to workspace; omit for workspace root)",
			},
		},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	// Virtual FS: route memory directory listing to DB
	if t.memIntc != nil {
		if listing, handled, err := t.memIntc.ListFiles(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to list memory files: %v", err))
			}
			if listing == "" {
				return SilentResult("No memory files stored yet")
			}
			return SilentResult(listing + "\n[Source: database, not filesystem]")
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, sandboxKey)
	}

	// Host execution — use per-user workspace from context if available
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	allowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, t.restrict), allowed)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := checkDeniedPath(resolved, t.workspace, t.deniedPrefixes); err != nil {
		return ErrorResult(err.Error())
	}

	entries, err := os.ReadDir(resolved)
	globalResolved := ""
	if !filepath.IsAbs(path) && path != "" {
		if globalWs := ToolTeamGlobalWorkspaceFromCtx(ctx); globalWs != "" && !sameCleanPath(globalWs, workspace) {
			if gr, grErr := resolvePathWithAllowed(path, globalWs, true, allowed); grErr == nil {
				globalResolved = gr
			}
		}
	}
	if err != nil {
		if os.IsNotExist(err) {
			if globalResolved != "" {
				if globalEntries, globalErr := os.ReadDir(globalResolved); globalErr == nil {
					return SilentResult(formatDirectoryEntries(globalResolved, globalEntries, t.deniedPrefixes, t.workspace))
				}
			}
			msg := fmt.Sprintf("Directory does not exist: %s", path)
			if teamWs := ToolTeamWorkspaceFromCtx(ctx); teamWs != "" && !strings.HasPrefix(resolved, teamWs) {
				msg += fmt.Sprintf("\nHint: try the team workspace path: list_files(path=\"%s/%s\")", teamWs, path)
			}
			return SilentResult(msg)
		}
		return ErrorResult(fmt.Sprintf("failed to list directory: %v", err))
	}

	if globalResolved != "" {
		if globalEntries, globalErr := os.ReadDir(globalResolved); globalErr == nil {
			return SilentResult(formatMergedDirectoryEntries(resolved, entries, globalResolved, globalEntries, t.deniedPrefixes, t.workspace))
		}
	}
	return SilentResult(formatDirectoryEntries(resolved, entries, t.deniedPrefixes, t.workspace))
}

func formatDirectoryEntries(resolved string, entries []os.DirEntry, deniedPrefixes []string, workspace string) string {
	var sb strings.Builder
	for _, entry := range entries {
		writeDirectoryEntry(&sb, resolved, entry, deniedPrefixes, workspace, "")
	}
	return sb.String()
}

func formatMergedDirectoryEntries(sessionDir string, sessionEntries []os.DirEntry, globalDir string, globalEntries []os.DirEntry, deniedPrefixes []string, workspace string) string {
	var sb strings.Builder
	seen := make(map[string]bool)
	for _, entry := range sessionEntries {
		if writeDirectoryEntry(&sb, sessionDir, entry, deniedPrefixes, workspace, "") {
			seen[entry.Name()] = true
		}
	}
	for _, entry := range globalEntries {
		if seen[entry.Name()] {
			continue
		}
		writeDirectoryEntry(&sb, globalDir, entry, deniedPrefixes, workspace, " [global]")
	}
	return sb.String()
}

func writeDirectoryEntry(sb *strings.Builder, dir string, entry os.DirEntry, deniedPrefixes []string, workspace, suffix string) bool {
	if len(deniedPrefixes) > 0 {
		entryPath := filepath.Join(dir, entry.Name())
		if checkDeniedPath(entryPath, workspace, deniedPrefixes) != nil {
			return false
		}
	}
	info, _ := entry.Info()
	if entry.IsDir() {
		fmt.Fprintf(sb, "[DIR]  %s/%s\n", entry.Name(), suffix)
	} else if info != nil {
		fmt.Fprintf(sb, "[FILE] %s (%d bytes)%s\n", entry.Name(), info.Size(), suffix)
	} else {
		fmt.Fprintf(sb, "[FILE] %s%s\n", entry.Name(), suffix)
	}
	return true
}

func (t *ListFilesTool) executeInSandbox(ctx context.Context, path, sandboxKey string) *Result {
	bridge, err := t.getFsBridge(ctx, sandboxKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	containerCwd, cwdErr := SandboxCwd(ctx, t.workspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("sandbox path mapping: %v", cwdErr))
	}
	containerPath := ResolveSandboxPath(path, containerCwd)

	output, err := bridge.ListDir(ctx, containerPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list directory: %v", err) + MaybeFsBridgeHint(err))
	}

	return SilentResult(output)
}

func (t *ListFilesTool) getFsBridge(ctx context.Context, sandboxKey string) (*sandbox.FsBridge, error) {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	return sandbox.NewFsBridge(sb.ID(), sandbox.DefaultContainerWorkdir), nil
}
