package tekshot

import (
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// topLeftZones mirrors what Drupal sends: one logo area per shape, as fractions.
func topLeftZones() map[string]any {
	zone := map[string]any{"mode": "logo", "left": 0.0, "top": 0.0, "width": 0.3, "height": 0.3}
	return map[string]any{
		"brand_zones": map[string]any{
			"square":    []any{zone},
			"portrait":  []any{zone},
			"landscape": []any{zone},
		},
	}
}

// skyImage is a smooth vertical gradient: the calm area the prompt asks for.
func skyImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		shade := uint8(150 + 60*y/height)
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{shade, shade + 30, 235, 255})
		}
	}
	return img
}

// drawTextLines paints dark horizontal strokes with gaps, the texture of a headline.
func drawTextLines(img *image.RGBA, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y += 14 {
		for x := x0; x < x1; x++ {
			if (x/9)%3 == 2 {
				continue
			}
			for dy := 0; dy < 6; dy++ {
				img.Set(x, y+dy, color.RGBA{20, 20, 30, 255})
			}
		}
	}
}

func writePNG(t *testing.T, img image.Image) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generated.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBrandZonesFromRequestReadsEveryShape(t *testing.T) {
	zones := brandZonesFromRequest(topLeftZones())
	if len(zones["square"]) != 1 || len(zones["portrait"]) != 1 || len(zones["landscape"]) != 1 {
		t.Fatalf("zones = %#v", zones)
	}
	if zones["square"][0].Width != 0.3 {
		t.Fatalf("width = %v", zones["square"][0].Width)
	}
	if got := brandZonesFromRequest(map[string]any{}); len(got) != 0 {
		t.Fatalf("no brand_zones must mean no check, got %#v", got)
	}
}

func TestBrandZoneShapeUsesDrupalThresholds(t *testing.T) {
	cases := map[[2]int]string{{1080, 1060}: "square", {1024, 1365}: "portrait", {1200, 628}: "landscape"}
	for size, want := range cases {
		if got := brandZoneShape(size[0], size[1]); got != want {
			t.Fatalf("%v: got %s want %s", size, got, want)
		}
	}
}

func TestCalmZoneIsNotBusy(t *testing.T) {
	path := writePNG(t, skyImage(1024, 1024))

	check, err := checkBrandZones(path, brandZonesFromRequest(topLeftZones()))
	if err != nil {
		t.Fatal(err)
	}
	if check.Busy {
		t.Fatalf("smooth sky must read as calm, score %.3f", check.Score)
	}
}

func TestTextInsideTheZoneIsBusy(t *testing.T) {
	img := skyImage(1024, 1024)
	drawTextLines(img, 40, 60, 290, 280)
	path := writePNG(t, img)

	check, err := checkBrandZones(path, brandZonesFromRequest(topLeftZones()))
	if err != nil {
		t.Fatal(err)
	}
	if !check.Busy {
		t.Fatalf("a headline inside the logo area must be caught, score %.3f", check.Score)
	}
	if check.AspectRatio != "1:1" {
		t.Fatalf("aspect = %s", check.AspectRatio)
	}
}

// Vân sàn, tường, vải: chi tiết mềm mà logo viền trắng đặt lên vẫn hài hoà.
// Sinh lại vì chúng là đốt lượt mà không làm ảnh đẹp hơn.
func TestSoftTextureIsNotBusy(t *testing.T) {
	img := skyImage(1024, 1024)
	for y := 0; y < 320; y++ {
		for x := 0; x < 320; x++ {
			if (x/6+y/6)%2 == 0 {
				c := img.RGBAAt(x, y)
				img.Set(x, y, color.RGBA{c.R - 12, c.G - 12, c.B - 12, 255})
			}
		}
	}
	path := writePNG(t, img)

	check, err := checkBrandZones(path, brandZonesFromRequest(topLeftZones()))
	if err != nil {
		t.Fatal(err)
	}
	if check.Busy {
		t.Fatalf("soft texture must not trigger a regeneration, score %.3f", check.Score)
	}
}

func TestTextOutsideTheZoneDoesNotTrigger(t *testing.T) {
	img := skyImage(1024, 1024)
	drawTextLines(img, 420, 600, 980, 980)
	path := writePNG(t, img)

	check, err := checkBrandZones(path, brandZonesFromRequest(topLeftZones()))
	if err != nil {
		t.Fatal(err)
	}
	if check.Busy {
		t.Fatalf("text elsewhere must not trigger a regeneration, score %.3f", check.Score)
	}
}

func TestJPEGBytesBehindAPNGNameStillDecode(t *testing.T) {
	// create_image always names the file .png, whatever the provider returned.
	img := skyImage(768, 1024)
	drawTextLines(img, 10, 10, 220, 290)
	path := filepath.Join(t.TempDir(), "generated.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	file.Close()

	check, err := checkBrandZones(path, brandZonesFromRequest(topLeftZones()))
	if err != nil {
		t.Fatal(err)
	}
	if !check.Busy || check.Shape != "portrait" || check.AspectRatio != "3:4" {
		t.Fatalf("check = %+v", check)
	}
}

func TestRegenerateOnlyWhenBusyAndAtMostOnce(t *testing.T) {
	original := []agent.MediaResult{{Path: "/ws/generated/a.png"}}
	fixCalls := 0
	fix := func(message string) ([]agent.MediaResult, error) {
		fixCalls++
		if !strings.Contains(message, "/ws/generated/a.png") || !strings.Contains(message, "aspect_ratio") {
			t.Fatalf("fix message must edit the produced image: %s", message)
		}
		return []agent.MediaResult{{Path: "/ws/generated/b.png"}, {Path: "/ws/generated/c.png"}}, nil
	}

	calm := func(string) (brandZoneCheck, error) { return brandZoneCheck{Busy: false}, nil }
	if got := regenerateForBrandZone(original, calm, fix); got[0].Path != "/ws/generated/a.png" || fixCalls != 0 {
		t.Fatalf("calm image must be kept untouched, got %v after %d fixes", got, fixCalls)
	}

	busy := func(string) (brandZoneCheck, error) {
		return brandZoneCheck{Busy: true, AspectRatio: "1:1", Zone: brandZone{Width: 0.3, Height: 0.3}}, nil
	}
	got := regenerateForBrandZone(original, busy, fix)
	if fixCalls != 1 || len(got) != 1 || got[0].Path != "/ws/generated/c.png" {
		t.Fatalf("busy image must be regenerated exactly once and keep one image, got %v after %d fixes", got, fixCalls)
	}
}

func TestFailedRegenerationKeepsTheOriginalImage(t *testing.T) {
	original := []agent.MediaResult{{Path: "/ws/generated/a.png"}}
	busy := func(string) (brandZoneCheck, error) { return brandZoneCheck{Busy: true, AspectRatio: "1:1"}, nil }

	failed := func(string) ([]agent.MediaResult, error) { return nil, errors.New("provider down") }
	if got := regenerateForBrandZone(original, busy, failed); got[0].Path != "/ws/generated/a.png" {
		t.Fatalf("a failed fix must not lose the image, got %v", got)
	}

	unreadable := func(string) (brandZoneCheck, error) { return brandZoneCheck{}, errors.New("decode") }
	neverFix := func(string) ([]agent.MediaResult, error) {
		t.Fatal("an unreadable image must not trigger a regeneration")
		return nil, nil
	}
	if got := regenerateForBrandZone(original, unreadable, neverFix); got[0].Path != "/ws/generated/a.png" {
		t.Fatalf("got %v", got)
	}
}
