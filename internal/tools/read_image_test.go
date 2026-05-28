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

func TestReadImageTool_LoadsUploadByBasenameFromSiblingScope(t *testing.T) {
	agentRoot := t.TempDir()
	workspace := filepath.Join(agentRoot, "1509083589399150602")
	uploadDir := filepath.Join(agentRoot, "guild_1501847620132405321_user_474714223667052546", ".uploads")
	name := "fb-apply-watermark-fdc52c0f1b70-8bed70c3.jpg"
	imagePath := filepath.Join(uploadDir, name)
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("not-a-real-jpg-but-readable"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadImageTool(nil)
	ctx := WithToolWorkspace(context.Background(), workspace)

	images, err := tool.loadImageFromPath(ctx, name)
	if err != nil {
		t.Fatalf("expected uploaded image basename fallback, got: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected one image, got %d", len(images))
	}
	if images[0].MimeType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", images[0].MimeType)
	}
}

func TestReadImageTool_AmbiguousUploadBasenameErrors(t *testing.T) {
	agentRoot := t.TempDir()
	workspace := filepath.Join(agentRoot, "1509083589399150602")
	name := "duplicate.jpg"
	for _, scope := range []string{"guild_a", "guild_b"} {
		path := filepath.Join(agentRoot, scope, ".uploads", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}

	tool := NewReadImageTool(nil)
	ctx := WithToolWorkspace(context.Background(), workspace)

	_, err := tool.loadImageFromPath(ctx, name)
	if err == nil {
		t.Fatal("expected ambiguous upload basename error")
	}
	if !strings.Contains(err.Error(), "multiple uploaded files") {
		t.Fatalf("expected ambiguity error, got: %v", err)
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
