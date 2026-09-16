package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// referencePathsFixture writes n distinguishable PNG files and returns a tool
// wired to a fake native provider plus the workspace-relative paths.
func referencePathsFixture(t *testing.T, n int) (*CreateImageTool, *nativeImageProvider, context.Context, []string) {
	t.Helper()

	pngMagic := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}

	workspace := t.TempDir()
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rel := filepath.Join("refs", string(rune('a'+i))+".png")
		abs := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatal(err)
		}
		// A trailing byte per file so each one's base64 differs and order is checkable.
		body := append(append([]byte(nil), pngMagic...), byte(i))
		if err := os.WriteFile(abs, body, 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.ToSlash(rel))
	}

	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: pngMagic,
	}
	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{"create_image": chainJSON})
	ctx = WithToolWorkspace(ctx, workspace)

	return NewCreateImageTool(reg), fakeProvider, ctx, paths
}

// trailingByte recovers the marker byte written by referencePathsFixture.
func trailingByte(t *testing.T, image providers.ImageContent) byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		t.Fatalf("reference image is not valid base64: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("reference image is empty")
	}
	return raw[len(raw)-1]
}

func TestCreateImageTool_ForwardsReferenceImagePathsInOrder(t *testing.T) {
	tool, fakeProvider, ctx, paths := referencePathsFixture(t, 3)

	result := tool.Execute(ctx, map[string]any{
		"prompt":                "image 1 is the base, image 2 is the reference",
		"reference_image_paths": []any{paths[0], paths[1], paths[2]},
	})
	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}
	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called on the native provider")
	}

	got := fakeProvider.calledWith.ReferenceImages
	if len(got) != 3 {
		t.Fatalf("ReferenceImages len = %d, want 3", len(got))
	}
	// Order is the only thing telling the image model which picture is the base.
	for i, image := range got {
		if marker := trailingByte(t, image); marker != byte(i) {
			t.Fatalf("ReferenceImages[%d] carries marker %d, want %d — order was not preserved", i, marker, i)
		}
		if image.MimeType != "image/png" {
			t.Fatalf("ReferenceImages[%d].MimeType = %q, want image/png", i, image.MimeType)
		}
	}
}

func TestCreateImageTool_ReferenceImagePathsOverrideSinglePath(t *testing.T) {
	tool, fakeProvider, ctx, paths := referencePathsFixture(t, 2)

	result := tool.Execute(ctx, map[string]any{
		"prompt":                "edit the base",
		"reference_image_path":  paths[1],
		"reference_image_paths": []any{paths[0], paths[1]},
	})
	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	got := fakeProvider.calledWith.ReferenceImages
	if len(got) != 2 {
		t.Fatalf("ReferenceImages len = %d, want 2 (the list must win over the single path)", len(got))
	}
	if marker := trailingByte(t, got[0]); marker != 0 {
		t.Fatalf("ReferenceImages[0] carries marker %d, want 0 — the list order must win", marker)
	}
}

func TestCreateImageTool_TrimsExcessReferenceImagePaths(t *testing.T) {
	tool, fakeProvider, ctx, paths := referencePathsFixture(t, maxReferenceImagePaths+2)

	given := make([]any, 0, len(paths))
	for _, p := range paths {
		given = append(given, p)
	}
	result := tool.Execute(ctx, map[string]any{
		"prompt":                "too many references",
		"reference_image_paths": given,
	})
	// Trimming, never refusing: losing the extra inputs beats losing the image.
	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}
	if len(fakeProvider.calledWith.ReferenceImages) != maxReferenceImagePaths {
		t.Fatalf("ReferenceImages len = %d, want %d", len(fakeProvider.calledWith.ReferenceImages), maxReferenceImagePaths)
	}
}

func TestCreateImageTool_EmptyReferenceImagePathsFallsBackToSinglePath(t *testing.T) {
	tool, fakeProvider, ctx, paths := referencePathsFixture(t, 2)

	result := tool.Execute(ctx, map[string]any{
		"prompt":                "single path still works",
		"reference_image_path":  paths[1],
		"reference_image_paths": []any{"", "   "},
	})
	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	got := fakeProvider.calledWith.ReferenceImages
	if len(got) != 1 {
		t.Fatalf("ReferenceImages len = %d, want 1", len(got))
	}
	if marker := trailingByte(t, got[0]); marker != 1 {
		t.Fatalf("ReferenceImages[0] carries marker %d, want 1", marker)
	}
}
