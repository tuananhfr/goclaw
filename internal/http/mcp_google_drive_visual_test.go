package http

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoogleDriveVisualPayload_FallbackCaption(t *testing.T) {
	payload, err := parseGoogleDriveVisualPayload("A product photo showing a red box on a white table.")
	if err != nil {
		t.Fatalf("parseGoogleDriveVisualPayload returned error: %v", err)
	}
	if payload.SummaryVI != "A product photo showing a red box on a white table." {
		t.Fatalf("SummaryVI = %q", payload.SummaryVI)
	}
	if payload.DescriptionVI != payload.SummaryVI {
		t.Fatalf("DescriptionVI = %q, want summary", payload.DescriptionVI)
	}
	if payload.SceneType != "other" {
		t.Fatalf("SceneType = %q, want other", payload.SceneType)
	}
	if payload.Quality != "medium" {
		t.Fatalf("Quality = %q, want medium", payload.Quality)
	}
	if !payload.UsableAsReference {
		t.Fatal("UsableAsReference = false, want true")
	}
}

func TestParseGoogleDriveVisualPayload_FallbackWrappedCaption(t *testing.T) {
	raw := `{"message":{"content":"The image shows shelves with packaged food products."}}`
	payload, err := parseGoogleDriveVisualPayload(raw)
	if err != nil {
		t.Fatalf("parseGoogleDriveVisualPayload returned error: %v", err)
	}
	if payload.SummaryVI != "The image shows shelves with packaged food products." {
		t.Fatalf("SummaryVI = %q", payload.SummaryVI)
	}
}

func TestParseGoogleDriveVisualPayload_PreservesStructuredJSON(t *testing.T) {
	raw := `{
		"summary_vi":"Anh san pham",
		"description_vi":"Mot anh san pham tren nen sang.",
		"tags_vi":["san pham"],
		"tags_en":["product"],
		"main_subject":"hop",
		"scene_type":"product",
		"detected_text":[],
		"usable_as_reference":true,
		"quality":"high"
	}`
	payload, err := parseGoogleDriveVisualPayload(raw)
	if err != nil {
		t.Fatalf("parseGoogleDriveVisualPayload returned error: %v", err)
	}
	if payload.SceneType != "product" {
		t.Fatalf("SceneType = %q, want product", payload.SceneType)
	}
	if payload.Quality != "high" {
		t.Fatalf("Quality = %q, want high", payload.Quality)
	}
	if len(payload.TagsVI) != 1 || payload.TagsVI[0] != "san pham" {
		t.Fatalf("TagsVI = %#v", payload.TagsVI)
	}
}

func TestPrepareGoogleDriveVisualImage_ResizesLargeImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.png")
	img := image.NewRGBA(image.Rect(0, 0, 2600, 2600))
	var seed uint32 = 1
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(seed >> 24),
				G: uint8(seed >> 16),
				B: uint8(seed >> 8),
				A: 255,
			})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("encode image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat image: %v", err)
	}
	if info.Size() <= googleDriveVisualMaxImageBytes {
		t.Fatalf("test image size = %d, want > %d", info.Size(), googleDriveVisualMaxImageBytes)
	}

	data, mime, err := prepareGoogleDriveVisualImage(path)
	if err != nil {
		t.Fatalf("prepareGoogleDriveVisualImage returned error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if len(data) == 0 || len(data) > googleDriveVisualMaxImageBytes {
		t.Fatalf("preview size = %d, want 1..%d", len(data), googleDriveVisualMaxImageBytes)
	}
}

func TestPrepareGoogleDriveVisualImage_UnsupportedType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.avif")
	if err := os.WriteFile(path, []byte("not an avif"), 0644); err != nil {
		t.Fatalf("write avif: %v", err)
	}
	_, _, err := prepareGoogleDriveVisualImage(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported image type") {
		t.Fatalf("error = %v, want unsupported image type", err)
	}
}
