package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileFallsBackToTeamGlobalWorkspace(t *testing.T) {
	root := t.TempDir()
	sessionWs := filepath.Join(root, "session")
	globalWs := filepath.Join(root, TeamGlobalScope)
	if err := os.MkdirAll(filepath.Join(globalWs, "brand-kits", "pizza-hips"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalWs, "brand-kits", "pizza-hips", "BRAND.md"), []byte("global brand"), 0640); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ctx = WithToolWorkspace(ctx, sessionWs)
	ctx = WithToolTeamWorkspace(ctx, sessionWs)
	ctx = WithToolTeamGlobalWorkspace(ctx, globalWs)

	tool := NewReadFileTool(sessionWs, true)
	result := tool.Execute(ctx, map[string]any{"path": "brand-kits/pizza-hips/BRAND.md"})
	if result.IsError {
		t.Fatalf("read_file returned error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "global brand") {
		t.Fatalf("expected global file content, got %q", result.ForLLM)
	}
}

func TestListFilesMergesTeamGlobalWorkspace(t *testing.T) {
	root := t.TempDir()
	sessionWs := filepath.Join(root, "session")
	globalWs := filepath.Join(root, TeamGlobalScope)
	if err := os.MkdirAll(sessionWs, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(globalWs, "brand-kits"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionWs, "generated.txt"), []byte("session"), 0640); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ctx = WithToolWorkspace(ctx, sessionWs)
	ctx = WithToolTeamWorkspace(ctx, sessionWs)
	ctx = WithToolTeamGlobalWorkspace(ctx, globalWs)

	tool := NewListFilesTool(sessionWs, true)
	result := tool.Execute(ctx, map[string]any{"path": "."})
	if result.IsError {
		t.Fatalf("list_files returned error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "generated.txt") {
		t.Fatalf("expected session file in listing, got %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "brand-kits/ [global]") {
		t.Fatalf("expected global directory in listing, got %q", result.ForLLM)
	}
}
