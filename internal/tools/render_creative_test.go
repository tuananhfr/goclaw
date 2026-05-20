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

	got := layer.withAutoLayout(0, 0, bounds)

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

func TestRenderCreativeAutoLayoutSeparatesLayers(t *testing.T) {
	bounds := imageRect(1080, 1080)
	headline := renderTextLayer{Text: "Tieu chuan quoc te", Layout: "auto", Size: 96}
	subtitle := renderTextLayer{Text: "Trong thiet ke ket cau", Layout: "auto", Size: 42}

	gotHeadline := headline.withAutoLayout(0, 0, bounds)
	gotSubtitle := subtitle.withAutoLayout(0, 1, bounds)

	if gotHeadline.X == gotSubtitle.X && gotHeadline.Y == gotSubtitle.Y && gotHeadline.Align == gotSubtitle.Align {
		t.Fatalf("auto layout placed two layers in the same zone: headline=%+v subtitle=%+v", gotHeadline, gotSubtitle)
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

	got := fitLayerToSafeBounds(fnt, layer, bounds, nil)
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

	topLogo := image.Rect(bounds.Dx()*30/100, 0, bounds.Dx()*72/100, bounds.Dy()*28/100)
	got := fitLayerToSafeBounds(fnt, layer, bounds, []image.Rectangle{topLogo})
	box, ok := measureLayerBounds(fnt, got)
	if !ok {
		t.Fatal("expected measurable layer")
	}
	if rectIntersects(box, topLogo) {
		t.Fatalf("text still intersects top-center watermark zone: box=%v logo=%v layer=%+v", box, topLogo, got)
	}
}

func TestRenderCreativeFitLayerRespectsExplicitPositionWithoutWatermarkZone(t *testing.T) {
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
		Text:        "TIEU CHUAN",
		X:           555,
		Y:           92,
		Size:        80,
		Align:       "left",
		MaxWidth:    430,
		StrokeWidth: 2,
	}

	got := fitLayerToSafeBounds(fnt, layer, bounds, nil)
	if got.X != layer.X || got.Y != layer.Y || got.Align != layer.Align {
		t.Fatalf("explicit layer was moved unexpectedly: got=%+v want=%+v", got, layer)
	}
}

func TestRenderCreativeFitLayerAvoidsConfiguredWatermarkForExplicitPosition(t *testing.T) {
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
	zone := image.Rect(480, 20, 760, 260)
	layer := renderTextLayer{
		Text:        "TIEU CHUAN",
		X:           555,
		Y:           92,
		Size:        80,
		Align:       "left",
		MaxWidth:    430,
		StrokeWidth: 2,
	}

	got := fitLayerToSafeBounds(fnt, layer, bounds, []image.Rectangle{zone})
	box, ok := measureLayerBounds(fnt, got)
	if !ok {
		t.Fatal("expected measurable layer")
	}
	if rectIntersects(box, zone) {
		t.Fatalf("explicit text still intersects configured watermark zone: box=%v zone=%v layer=%+v", box, zone, got)
	}
}

func TestRenderCreativeWatermarkAvoidZonesFromArgs(t *testing.T) {
	bounds := imageRect(1080, 1080)
	zones := watermarkAvoidZonesFromArgs(map[string]any{
		"watermark": map[string]any{
			"enabled":   true,
			"mode":      "logo",
			"x_pct":     0.1,
			"y_pct":     0.1,
			"scale_pct": 0.2,
		},
	}, bounds)

	if len(zones) != 1 {
		t.Fatalf("expected one watermark avoid zone, got %d", len(zones))
	}
	zone := zones[0]
	if zone.Min.X > 108 || zone.Max.X < 108 || zone.Min.Y > 108 || zone.Max.Y < 108 {
		t.Fatalf("avoid zone does not contain configured watermark center: %v", zone)
	}
	if zone.Dx() <= 216 || zone.Dy() <= 216 {
		t.Fatalf("avoid zone should include scale plus padding, got %v", zone)
	}
}

func imageRect(width, height int) image.Rectangle {
	return image.Rect(0, 0, width, height)
}
