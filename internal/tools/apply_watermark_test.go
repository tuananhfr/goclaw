package tools

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWatermarkTool_DisabledUsesOriginal(t *testing.T) {
	workspace := t.TempDir()
	base := filepath.Join(workspace, "base.png")
	writeSolidPNG(t, base, 40, 40, color.RGBA{255, 255, 255, 255})

	tool := NewApplyWatermarkTool(workspace, true)
	result := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"watermark": map[string]any{
			"enabled": false,
			"mode":    "logo",
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "has_watermark: false") {
		t.Fatalf("expected has_watermark false, got %q", result.ForLLM)
	}
	if len(result.Media) != 1 || result.Media[0].Path != base {
		t.Fatalf("expected original media path %q, got %+v", base, result.Media)
	}
}

func TestApplyWatermarkTool_LogoWatermarkTrimsPadding(t *testing.T) {
	workspace := t.TempDir()
	base := filepath.Join(workspace, "base.png")
	logo := filepath.Join(workspace, "logo.png")
	out := filepath.Join(workspace, "out.png")
	writeSolidPNG(t, base, 100, 100, color.RGBA{255, 255, 255, 255})
	writePaddedLogoPNG(t, logo)

	tool := NewApplyWatermarkTool(workspace, true)
	result := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"output_path":     "out.png",
		"watermark": map[string]any{
			"enabled":   true,
			"mode":      "logo",
			"logo_path": "logo.png",
			"x_pct":     0.5,
			"y_pct":     0.5,
			"scale_pct": 0.2,
			"opacity":   1,
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	img := readPNG(t, out)
	if got := countDominantPixels(img, func(c color.RGBA) bool { return c.R > 220 && c.G < 40 && c.B < 40 }); got < 300 {
		t.Fatalf("expected trimmed red logo to occupy most resized watermark area, got %d red pixels", got)
	}
}

func TestApplyWatermarkTool_TextWatermark(t *testing.T) {
	workspace := t.TempDir()
	base := filepath.Join(workspace, "base.png")
	out := filepath.Join(workspace, "text.png")
	writeSolidPNG(t, base, 120, 80, color.RGBA{255, 255, 255, 255})

	tool := NewApplyWatermarkTool(workspace, true)
	result := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"output_path":     "text.png",
		"watermark": map[string]any{
			"enabled":   true,
			"mode":      "text",
			"text":      "ACME",
			"x_pct":     0.5,
			"y_pct":     0.5,
			"scale_pct": 0.4,
			"opacity":   1,
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	img := readPNG(t, out)
	if got := countDominantPixels(img, func(c color.RGBA) bool { return c.R < 250 || c.G < 250 || c.B < 250 }); got == 0 {
		t.Fatal("expected text watermark to change pixels")
	}
}

func TestApplyWatermarkTool_ItemsApplySequentially(t *testing.T) {
	workspace := t.TempDir()
	base := filepath.Join(workspace, "base.png")
	redLogo := filepath.Join(workspace, "red.png")
	blueLogo := filepath.Join(workspace, "blue.png")
	out := filepath.Join(workspace, "items.png")
	writeSolidPNG(t, base, 100, 100, color.RGBA{255, 255, 255, 255})
	writeSolidPNG(t, redLogo, 10, 10, color.RGBA{255, 0, 0, 255})
	writeSolidPNG(t, blueLogo, 10, 10, color.RGBA{0, 0, 255, 255})

	tool := NewApplyWatermarkTool(workspace, true)
	result := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"output_path":     "items.png",
		"watermark": map[string]any{
			"enabled": true,
			"items": []any{
				map[string]any{"enabled": true, "mode": "logo", "logo_path": "red.png", "x_pct": 0.25, "y_pct": 0.5, "scale_pct": 0.2, "opacity": 1},
				map[string]any{"enabled": true, "mode": "logo", "logo_path": "blue.png", "x_pct": 0.75, "y_pct": 0.5, "scale_pct": 0.2, "opacity": 1},
			},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	img := readPNG(t, out)
	reds := countDominantPixels(img, func(c color.RGBA) bool { return c.R > 220 && c.G < 40 && c.B < 40 })
	blues := countDominantPixels(img, func(c color.RGBA) bool { return c.B > 220 && c.R < 40 && c.G < 40 })
	if reds == 0 || blues == 0 {
		t.Fatalf("expected both red and blue watermarks, got red=%d blue=%d", reds, blues)
	}
}

func TestApplyWatermarkTool_MissingAssetsError(t *testing.T) {
	workspace := t.TempDir()
	base := filepath.Join(workspace, "base.png")
	writeSolidPNG(t, base, 40, 40, color.RGBA{255, 255, 255, 255})
	tool := NewApplyWatermarkTool(workspace, true)

	logoResult := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"watermark":      map[string]any{"enabled": true, "mode": "logo"},
	})
	if !logoResult.IsError || !strings.Contains(logoResult.ForLLM, "logo_path or logo_url") {
		t.Fatalf("expected missing logo error, got %+v", logoResult)
	}

	textResult := tool.Execute(WithToolWorkspace(context.Background(), workspace), map[string]any{
		"base_image_path": "base.png",
		"watermark":      map[string]any{"enabled": true, "mode": "text"},
	})
	if !textResult.IsError || !strings.Contains(textResult.ForLLM, "non-empty text") {
		t.Fatalf("expected missing text error, got %+v", textResult)
	}
}

func writeSolidPNG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	writePNG(t, path, img)
}

func writePaddedLogoPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	draw.Draw(img, image.Rect(15, 15, 25, 25), &image.Uniform{C: color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	writePNG(t, path, img)
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func countDominantPixels(img image.Image, match func(color.RGBA) bool) int {
	b := img.Bounds()
	count := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if match(color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)) {
				count++
			}
		}
	}
	return count
}
