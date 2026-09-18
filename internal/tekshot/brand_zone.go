package tekshot

import (
	"fmt"
	"image"
	_ "image/jpeg" // create_image names every file .png; decode by content.
	_ "image/png"
	"log/slog"
	"math"
	"os"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// brandZone là vùng Drupal sẽ đóng logo (StudioBrandZones::zone), theo phần
// trăm ảnh, đã gồm khoảng đệm quanh logo.
type brandZone struct {
	Left, Top, Width, Height float64
}

type brandZoneCheck struct {
	Busy bool
	// Patch: vùng logo phẳng nhưng khác màu hẳn với nền quanh nó — model vẽ một
	// mảng/khung nền riêng chờ logo, logo dán lên trông như miếng vá.
	Patch       bool
	PatchDelta  float64
	Score       float64
	Shape       string
	AspectRatio string
	Zone        brandZone
}

const (
	// Lưới lấy mẫu tối đa theo cạnh dài của vùng; đủ mịn để nét chữ headline
	// cỡ đọc được trên điện thoại vẫn còn là cạnh.
	brandZoneSampleMax = 96
	// Chênh sáng giữa hai ô kề nhau từ mức này mới tính là một cạnh: gradient
	// trời và nhiễu JPEG nằm dưới, nét chữ và chi tiết vật thể nằm trên.
	brandZoneEdgeDelta = 18.0
	// Tỉ lệ cặp ô kề nhau là cạnh từ mức này thì vùng coi là bị chiếm. Chốt
	// 2026-09-14 trên 465 vùng của 93 ảnh thật: chữ thấp nhất 0.097 (chữ trắng
	// trên nền phẳng), mặt người và vật thể 0.10-0.12, vân sàn/tường dưới 0.09.
	brandZoneBusyScore = 0.09
	// Mảng nền riêng: trung bình màu của vùng lệch khỏi dải viền quanh nó từ
	// mức này (khoảng cách RGB), trong khi cả vùng lẫn viền đều phẳng. Nền tự
	// nhiên (trời, tường, đường) liền mạch nên lệch dưới mức này; khung trắng
	// hay khối màu vẽ sẵn chờ logo lệch rất xa.
	brandZonePatchDelta = 48.0
	// Độ lệch chuẩn độ sáng tối đa để coi một vùng là phẳng.
	brandZonePatchFlatStd = 24.0
	// Số lần sửa tối đa cho một ảnh: một lần đôi khi chưa đủ.
	brandZoneMaxFixes = 2
)

var brandZoneShapes = []string{"square", "portrait", "landscape"}

func brandZonesFromRequest(request map[string]any) map[string][]brandZone {
	raw, ok := request["brand_zones"].(map[string]any)
	if !ok {
		return nil
	}
	zones := make(map[string][]brandZone)
	for _, shape := range brandZoneShapes {
		for _, item := range anySlice(raw[shape]) {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			zone := brandZone{
				Left:   numberFromMap(record, "left"),
				Top:    numberFromMap(record, "top"),
				Width:  numberFromMap(record, "width"),
				Height: numberFromMap(record, "height"),
			}
			if zone.Width > 0 && zone.Height > 0 {
				zones[shape] = append(zones[shape], zone)
			}
		}
	}
	if len(zones) == 0 {
		return nil
	}
	return zones
}

// brandZoneShape dùng đúng hai ngưỡng của StudioBrandZones::shapeFor.
func brandZoneShape(width, height int) string {
	ratio := 1.0
	if height > 0 {
		ratio = float64(width) / float64(height)
	}
	switch {
	case ratio >= 1.05:
		return "landscape"
	case ratio <= 0.95:
		return "portrait"
	default:
		return "square"
	}
}

// brandZoneAspectRatio là tỉ lệ create_image gần nhất với kích thước thật.
func brandZoneAspectRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return "1:1"
	}
	actual := float64(width) / float64(height)
	best, bestDiff := "1:1", math.MaxFloat64
	for _, candidate := range []struct {
		label string
		ratio float64
	}{{"1:1", 1}, {"3:4", 0.75}, {"4:3", 4.0 / 3}, {"9:16", 9.0 / 16}, {"16:9", 16.0 / 9}} {
		if diff := math.Abs(math.Log(actual / candidate.ratio)); diff < bestDiff {
			best, bestDiff = candidate.label, diff
		}
	}
	return best
}

// checkBrandZones đo vùng logo trên ảnh vừa sinh; vùng rối nhất quyết định.
func checkBrandZones(path string, zones map[string][]brandZone) (brandZoneCheck, error) {
	file, err := os.Open(path)
	if err != nil {
		return brandZoneCheck{}, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return brandZoneCheck{}, fmt.Errorf("decode %s: %w", path, err)
	}
	bounds := img.Bounds()
	check := brandZoneCheck{
		Shape:       brandZoneShape(bounds.Dx(), bounds.Dy()),
		AspectRatio: brandZoneAspectRatio(bounds.Dx(), bounds.Dy()),
	}
	var patchZone brandZone
	for _, zone := range zones[check.Shape] {
		if score := brandZoneScore(img, zone); score > check.Score || check.Zone == (brandZone{}) {
			check.Score, check.Zone = score, zone
		}
		if patch, delta := brandZonePatch(img, zone); patch && delta > check.PatchDelta {
			check.Patch, check.PatchDelta, patchZone = true, delta, zone
		}
	}
	check.Busy = check.Score >= brandZoneBusyScore
	if !check.Busy && check.Patch {
		check.Zone = patchZone
	}
	return check, nil
}

// needsFix: vùng logo bị chữ/chi tiết chiếm, hoặc bị tô một mảng màu riêng.
func (c brandZoneCheck) needsFix() bool {
	return c.Busy || c.Patch
}

type colorStats struct {
	r, g, b, lumSum, lumSq float64
	n                      int
}

func (c *colorStats) add(img image.Image, x, y int) {
	r, g, b, _ := img.At(x, y).RGBA()
	rf, gf, bf := float64(r>>8), float64(g>>8), float64(b>>8)
	lum := 0.2126*rf + 0.7152*gf + 0.0722*bf
	c.r += rf
	c.g += gf
	c.b += bf
	c.lumSum += lum
	c.lumSq += lum * lum
	c.n++
}

func (c colorStats) mean() (float64, float64, float64) {
	n := float64(max(1, c.n))
	return c.r / n, c.g / n, c.b / n
}

func (c colorStats) lumStd() float64 {
	if c.n == 0 {
		return 0
	}
	n := float64(c.n)
	mean := c.lumSum / n
	return math.Sqrt(math.Max(0, c.lumSq/n-mean*mean))
}

// brandZonePatch so màu vùng logo với dải viền quanh nó (rộng bằng nửa vùng).
// Chỉ báo khi vùng và viền đều phẳng mà màu lệch nhau xa: đó là mảng nền riêng
// model tô chờ logo, không phải cảnh tự nhiên.
func brandZonePatch(img image.Image, zone brandZone) (bool, float64) {
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())
	zx0, zy0 := zone.Left*w, zone.Top*h
	zx1, zy1 := (zone.Left+zone.Width)*w, (zone.Top+zone.Height)*h
	if zx1-zx0 < 8 || zy1-zy0 < 8 {
		return false, 0
	}
	padX, padY := (zx1-zx0)/2, (zy1-zy0)/2
	rx0, ry0 := math.Max(0, zx0-padX), math.Max(0, zy0-padY)
	rx1, ry1 := math.Min(w, zx1+padX), math.Min(h, zy1+padY)

	step := max(1, int(math.Max(rx1-rx0, ry1-ry0))/brandZoneSampleMax)
	var inside, ring colorStats
	for y := int(ry0); y < int(ry1); y += step {
		for x := int(rx0); x < int(rx1); x += step {
			fx, fy := float64(x), float64(y)
			if fx >= zx0 && fx < zx1 && fy >= zy0 && fy < zy1 {
				inside.add(img, bounds.Min.X+x, bounds.Min.Y+y)
			} else {
				ring.add(img, bounds.Min.X+x, bounds.Min.Y+y)
			}
		}
	}
	if inside.n < 16 || ring.n < 16 {
		return false, 0
	}
	ir, ig, ib := inside.mean()
	or, og, ob := ring.mean()
	delta := math.Sqrt((ir-or)*(ir-or) + (ig-og)*(ig-og) + (ib-ob)*(ib-ob))
	flat := inside.lumStd() <= brandZonePatchFlatStd && ring.lumStd() <= brandZonePatchFlatStd
	return flat && delta >= brandZonePatchDelta, delta
}

// brandZoneScore là tỉ lệ cặp ô kề nhau có chênh sáng đủ lớn để là một cạnh.
func brandZoneScore(img image.Image, zone brandZone) float64 {
	bounds := img.Bounds()
	x0 := bounds.Min.X + int(math.Round(zone.Left*float64(bounds.Dx())))
	y0 := bounds.Min.Y + int(math.Round(zone.Top*float64(bounds.Dy())))
	x1 := bounds.Min.X + int(math.Round((zone.Left+zone.Width)*float64(bounds.Dx())))
	y1 := bounds.Min.Y + int(math.Round((zone.Top+zone.Height)*float64(bounds.Dy())))
	x0, y0 = max(x0, bounds.Min.X), max(y0, bounds.Min.Y)
	x1, y1 = min(x1, bounds.Max.X), min(y1, bounds.Max.Y)
	width, height := x1-x0, y1-y0
	if width < 4 || height < 4 {
		return 0
	}

	cell := max(1, (max(width, height)+brandZoneSampleMax-1)/brandZoneSampleMax)
	cols, rows := width/cell, height/cell
	if cols < 2 || rows < 2 {
		return 0
	}
	grid := make([]float64, cols*rows)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			var sum float64
			for dy := 0; dy < cell; dy++ {
				for dx := 0; dx < cell; dx++ {
					r, g, b, _ := img.At(x0+col*cell+dx, y0+row*cell+dy).RGBA()
					sum += 0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(b>>8)
				}
			}
			grid[row*cols+col] = sum / float64(cell*cell)
		}
	}

	edges, pairs := 0, 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			value := grid[row*cols+col]
			if col+1 < cols {
				pairs++
				if math.Abs(value-grid[row*cols+col+1]) >= brandZoneEdgeDelta {
					edges++
				}
			}
			if row+1 < rows {
				pairs++
				if math.Abs(value-grid[(row+1)*cols+col]) >= brandZoneEdgeDelta {
					edges++
				}
			}
		}
	}
	return float64(edges) / float64(pairs)
}

// regenerateForBrandZone sửa ảnh khi vùng logo bị chữ hay chi tiết chiếm, hoặc
// bị tô một mảng màu khác nền; tối đa brandZoneMaxFixes lần, đo lại sau mỗi lần.
// Đo không được hoặc sửa hỏng thì giữ ảnh đang có: mất ảnh tệ hơn logo đè.
func regenerateForBrandZone(
	media []agent.MediaResult,
	check func(path string) (brandZoneCheck, error),
	fix func(message string) ([]agent.MediaResult, error),
) []agent.MediaResult {
	if len(media) == 0 || strings.TrimSpace(media[0].Path) == "" {
		return media
	}
	path := media[0].Path
	result, err := check(path)
	if err != nil {
		slog.Warn("tekshot brand zone: cannot measure generated image", "path", path, "error", err)
		return media
	}
	if !result.needsFix() {
		// Ghi cả lượt sạch: không có dòng này thì "đã đo, sạch" và "không hề đo"
		// trông giống hệt nhau trong log.
		slog.Info("tekshot brand zone: logo area is calm", "path", path, "score", result.Score, "shape", result.Shape)
		return media
	}
	current := media
	for attempt := 1; attempt <= brandZoneMaxFixes; attempt++ {
		slog.Info("tekshot brand zone: logo area needs a fix, regenerating",
			"path", path, "attempt", attempt, "busy", result.Busy, "score", result.Score,
			"patch", result.Patch, "patch_delta", result.PatchDelta, "shape", result.Shape)
		fixed, err := fix(brandZoneFixMessage(path, result))
		if err != nil || len(fixed) == 0 {
			slog.Warn("tekshot brand zone: regeneration failed, keeping the current image", "path", path, "error", err)
			return current
		}
		current = fixed[len(fixed)-1:]
		path = current[0].Path
		if attempt == brandZoneMaxFixes {
			break
		}
		next, err := check(path)
		if err != nil {
			slog.Warn("tekshot brand zone: cannot measure fixed image", "path", path, "error", err)
			return current
		}
		if !next.needsFix() {
			slog.Info("tekshot brand zone: logo area is calm after fix", "path", path, "attempt", attempt, "score", next.Score)
			return current
		}
		result = next
	}
	return current
}

func brandZoneFixMessage(path string, check brandZoneCheck) string {
	if !check.Busy && check.Patch {
		return fmt.Sprintf("[System] You must call create_image now — do not reply with plain text. "+
			"The image you just produced has a separately coloured patch, panel or block in the brand mark area: the %s, from %d%% to %d%% of the width (from the left) and %d%% to %d%% of the height (from the top), where the brand logo is placed in post-production. "+
			"Edit that image: set reference_image_path to exactly %q and aspect_ratio to %q. "+
			"Keep the composition, subject, colours, headline wording and every other element as they are, but repaint that area as a seamless continuation of the surrounding background — the same colour, brightness, gradient and texture as the image right next to it — with no patch, panel, frame, badge or colour change. "+
			"Do not draw a logo, frame, box or placeholder anywhere.",
			brandZonePositionLabel(check.Zone),
			int(math.Floor(check.Zone.Left*100)), int(math.Ceil((check.Zone.Left+check.Zone.Width)*100)),
			int(math.Floor(check.Zone.Top*100)), int(math.Ceil((check.Zone.Top+check.Zone.Height)*100)),
			path,
			check.AspectRatio,
		)
	}
	return fmt.Sprintf("[System] You must call create_image now — do not reply with plain text. "+
		"The image you just produced has text or busy detail inside the brand mark area: the %s, about %d%% of the width and %d%% of the height, where the brand logo is placed in post-production. "+
		"Edit that image: set reference_image_path to exactly %q and aspect_ratio to %q. "+
		"Keep the composition, subject, colours, headline wording and every other element as they are, but move any text, label or important detail out of that area and let the area become a calm, uncluttered continuation of the surrounding background, with the same colour and texture as the image right next to it (no patch or panel). "+
		"Do not draw a logo, frame, box or placeholder anywhere.",
		brandZonePositionLabel(check.Zone),
		int(math.Round(check.Zone.Width*100)),
		int(math.Round(check.Zone.Height*100)),
		path,
		check.AspectRatio,
	)
}

// brandZonePositionLabel dùng đúng cách gọi vị trí của StudioBrandZones.
func brandZonePositionLabel(zone brandZone) string {
	x := (zone.Left + zone.Width/2) * 100
	y := (zone.Top + zone.Height/2) * 100
	horizontal, vertical := "centre", "middle"
	if x <= 33 {
		horizontal = "left"
	} else if x >= 67 {
		horizontal = "right"
	}
	if y <= 33 {
		vertical = "top"
	} else if y >= 67 {
		vertical = "bottom"
	}
	switch {
	case horizontal == "centre" && vertical == "middle":
		return "centre of the image"
	case horizontal == "centre":
		return vertical + " edge, horizontally centred"
	case vertical == "middle":
		return horizontal + " edge, vertically centred"
	default:
		return vertical + "-" + horizontal + " corner"
	}
}
