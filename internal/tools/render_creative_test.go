package tools

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCreativeTool_RendersFontImage(t *testing.T) {
	workspace := t.TempDir()
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fontPath); err != nil {
		t.Skipf("test font not available: %v", err)
	}

	tool := NewRenderCreativeTool(workspace, true)
	tool.AllowPaths(filepath.Dir(fontPath))
	ctx := WithToolWorkspace(context.Background(), workspace)
	outPath := filepath.Join("generated", "brand-final.png")

	result := tool.Execute(ctx, map[string]any{
		"output_path": outPath,
		"font_path":   fontPath,
		"width":       320,
		"height":      180,
		"texts": []any{
			map[string]any{
				"text":         "PIZZA HIPS",
				"layout":       "auto",
				"size":         float64(42),
				"color":        "#FFD84A",
				"stroke_color": "#E53935",
				"stroke_width": float64(2),
			},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected render error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "font_sha256:") {
		t.Fatalf("result missing font_sha256: %s", result.ForLLM)
	}
	finalPath := filepath.Join(workspace, outPath)
	f, err := os.Open(finalPath)
	if err != nil {
		t.Fatalf("rendered file missing: %v", err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("rendered file is not a valid PNG: %v", err)
	}
	if len(result.Media) != 1 || result.Media[0].Path != finalPath {
		t.Fatalf("expected primary media path %q, got %+v", finalPath, result.Media)
	}
}

func TestRenderCreativeTool_IgnoresVariantsUnlessAllowed(t *testing.T) {
	workspace := t.TempDir()
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fontPath); err != nil {
		t.Skipf("test font not available: %v", err)
	}

	tool := NewRenderCreativeTool(workspace, true)
	tool.AllowPaths(filepath.Dir(fontPath))
	ctx := WithToolWorkspace(context.Background(), workspace)

	result := tool.Execute(ctx, map[string]any{
		"output_path": "generated/brand-final.png",
		"font_path":   fontPath,
		"width":       320,
		"height":      180,
		"variants":    float64(5),
		"texts": []any{
			map[string]any{
				"text":   "PIZZA HIPS",
				"layout": "auto",
				"size":   float64(42),
			},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected render error: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "variants:") {
		t.Fatalf("expected one final image when allow_variants is false, got: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected one media file, got %+v", result.Media)
	}
	if _, err := os.Stat(filepath.Join(workspace, "generated", "brand-final_v2.png")); !os.IsNotExist(err) {
		t.Fatalf("unexpected second variant file exists or stat failed: %v", err)
	}
}
