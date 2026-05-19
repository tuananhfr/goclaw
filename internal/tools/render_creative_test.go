package tools

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/opentype"
)

func TestRenderCreativeTool_RendersFontImage(t *testing.T) {
	workspace := t.TempDir()
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "pages", "pizza-hips", "skills", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
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
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "pages", "pizza-hips", "skills", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
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

func TestRenderCreativeAutoLayoutAvoidsTopCenterWatermark(t *testing.T) {
	bounds := imageRect(1080, 1080)
	layer := renderTextLayer{Text: "PIZZA", Layout: "auto", Size: 96}

	got := layer.withAutoLayout(0, bounds)

	if got.Align != "left" {
		t.Fatalf("default auto layout align = %q, want left", got.Align)
	}
	if got.X > bounds.Dx()/3 {
		t.Fatalf("default auto layout X = %d, want outside top-center watermark zone", got.X)
	}
	if got.Y < bounds.Dy()*24/100 {
		t.Fatalf("default auto layout Y = %d, want below top watermark zone", got.Y)
	}
	if got.MaxWidth > bounds.Dx()/2 {
		t.Fatalf("default auto layout max width = %d, want narrow enough to avoid center watermark", got.MaxWidth)
	}
}

func TestRenderCreativeFitLayerPreventsTopClipping(t *testing.T) {
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "pages", "pizza-hips", "skills", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fontPath)
	if err != nil {
		t.Skipf("test font not available: %v", err)
	}
	fnt, err := opentype.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	bounds := imageRect(1080, 1080)
	layer := renderTextLayer{
		Text:        "PIZZA\nNONG\nHOI",
		X:           120,
		Y:           20,
		Size:        150,
		Align:       "left",
		MaxWidth:    500,
		StrokeWidth: 4,
	}

	got := fitLayerToSafeBounds(fnt, layer, bounds)
	box, ok := measureLayerBounds(fnt, got)
	if !ok {
		t.Fatal("expected measurable layer")
	}
	if box.Min.Y < bounds.Min.Y+bounds.Dy()*5/100 {
		t.Fatalf("text still clips top safe margin: box=%v", box)
	}
}

func TestRenderCreativeFitLayerAvoidsTopCenterWatermark(t *testing.T) {
	fontPath, err := filepath.Abs(filepath.Join("..", "..", "skill-drafts", "pages", "pizza-hips", "skills", "brand-pizza-hips-guidelines", "assets", "fonts", "SVN-Bango.otf"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fontPath)
	if err != nil {
		t.Skipf("test font not available: %v", err)
	}
	fnt, err := opentype.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	bounds := imageRect(1080, 1080)
	layer := renderTextLayer{
		Text:        "PIZZA\nNONG\nHOI",
		X:           170,
		Y:           120,
		Size:        150,
		Align:       "left",
		MaxWidth:    520,
		StrokeWidth: 4,
	}

	got := fitLayerToSafeBounds(fnt, layer, bounds)
	box, ok := measureLayerBounds(fnt, got)
	if !ok {
		t.Fatal("expected measurable layer")
	}
	topLogo := image.Rect(bounds.Dx()*30/100, 0, bounds.Dx()*72/100, bounds.Dy()*28/100)
	if rectIntersects(box, topLogo) {
		t.Fatalf("text still intersects top-center watermark zone: box=%v logo=%v layer=%+v", box, topLogo, got)
	}
}

func imageRect(width, height int) image.Rectangle {
	return image.Rect(0, 0, width, height)
}
