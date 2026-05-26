package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadImageTool_LoadImageFromAllowedSkillDir(t *testing.T) {
	workspace := t.TempDir()
	skillDir := t.TempDir()
	imagePath := filepath.Join(skillDir, "assets", "sample.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("not-a-real-png-but-readable"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadImageTool(nil)
	tool.AllowPaths(skillDir)

	ctx := WithToolWorkspace(context.Background(), workspace)
	images, err := tool.loadImageFromPath(ctx, imagePath)
	if err != nil {
		t.Fatalf("expected allowed skill image path to load, got: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected one image, got %d", len(images))
	}
	if images[0].MimeType != "image/png" {
		t.Fatalf("expected image/png, got %q", images[0].MimeType)
	}
}

func TestReadImageTool_LoadsJFIFAsJPEG(t *testing.T) {
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, "generated", "sample.jfif")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("not-a-real-jfif-but-readable"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadImageTool(nil)
	ctx := WithToolWorkspace(context.Background(), workspace)

	images, err := tool.loadImageFromPath(ctx, "generated/sample.jfif")
	if err != nil {
		t.Fatalf("expected jfif image path to load, got: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected one image, got %d", len(images))
	}
	if images[0].MimeType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", images[0].MimeType)
	}
}

func TestReadImageTool_BlocksParentTraversal(t *testing.T) {
	workspace := t.TempDir()
	tool := NewReadImageTool(nil)
	ctx := WithToolWorkspace(context.Background(), workspace)

	_, err := tool.loadImageFromPath(ctx, "../outside.png")
	if err == nil {
		t.Fatal("expected parent traversal to be blocked")
	}
	if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("expected outside workspace error, got: %v", err)
	}
}
