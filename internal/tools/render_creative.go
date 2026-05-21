package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type RenderCreativeTool struct {
	workspace       string
	restrict        bool
	allowedPrefixes []string
}

func NewRenderCreativeTool(workspace string, restrict bool) *RenderCreativeTool {
	return &RenderCreativeTool{workspace: workspace, restrict: restrict}
}

func (t *RenderCreativeTool) Name() string { return "render_creative" }

func (t *RenderCreativeTool) Description() string {
	return "Render text into a flattened PNG image using a real .otf/.ttf font file. Use after image generation when brand-font accuracy is required."
}

func (t *RenderCreativeTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}

func (t *RenderCreativeTool) AllowedPaths() []string {
	return append([]string(nil), t.allowedPrefixes...)
}

func (t *RenderCreativeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"base_image_path": map[string]any{
				"type":        "string",
				"description": "Optional background image path in workspace/team workspace. If this comes from create_image, call create_image with deliver=false so only the final rendered creative is attached.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Output PNG path in workspace/team workspace, e.g. 'page1-final.png'.",
			},
			"font_path": map[string]any{
				"type":        "string",
				"description": "Path to a real .otf/.ttf font file, usually under brand-kits/<brand>/assets/fonts/.",
			},
			"width": map[string]any{
				"type":        "integer",
				"description": "Canvas width when no base_image_path is provided. Default 1080.",
			},
			"height": map[string]any{
				"type":        "integer",
				"description": "Canvas height when no base_image_path is provided. Default 1080.",
			},
			"texts": map[string]any{
				"type":        "array",
				"description": "Text layers to render. Each layer supports text, x, y, size, color, align, max_width, stroke_color, stroke_width, layout.",
				"items": map[string]any{
					"type": "object",
				},
			},
			"variants": map[string]any{
				"type":        "integer",
				"description": "Number of layout variants to render when layer layout is 'auto'. Default 1. Values above 1 are ignored unless allow_variants is true.",
			},
			"allow_variants": map[string]any{
				"type":        "boolean",
				"description": "Set true only when the user explicitly asks for comparison variants. Otherwise render_creative returns one final image.",
			},
			"watermark": map[string]any{
				"type":        "object",
				"description": "Optional watermark config from fb_get_watermark_config. When present, text is kept outside the configured watermark overlay zones.",
			},
		},
		"required": []string{"output_path", "font_path", "texts"},
	}
}

func (t *RenderCreativeTool) Execute(ctx context.Context, args map[string]any) *Result {
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	if workspace == "" {
		workspace = os.TempDir()
	}

	fontPath, _ := args["font_path"].(string)
	if fontPath == "" {
		return ErrorResult("font_path is required")
	}
	outputPath, _ := args["output_path"].(string)
	if outputPath == "" {
		return ErrorResult("output_path is required")
	}
	rawTexts, ok := args["texts"].([]any)
	if !ok || len(rawTexts) == 0 {
		return ErrorResult("texts must be a non-empty array")
	}

	readAllowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	resolvedFont, err := resolveReadPathWithGlobalOverlay(ctx, fontPath, workspace, effectiveRestrict(ctx, t.restrict), readAllowed)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid font_path: %v", err))
	}
	fontData, err := os.ReadFile(resolvedFont)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read font_path: %v", err))
	}
	parsedFont, err := opentype.Parse(fontData)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse font_path as .otf/.ttf: %v", err))
	}
	fontHash := sha256.Sum256(fontData)
	fontSHA := strings.ToUpper(hex.EncodeToString(fontHash[:]))

	base, err := t.loadBaseImage(ctx, args, workspace, readAllowed)
	if err != nil {
		return ErrorResult(err.Error())
	}
	layers := parseRenderTextLayers(rawTexts)
	if len(layers) == 0 {
		return ErrorResult("texts must contain at least one layer with non-empty text")
	}
	avoidZones := watermarkAvoidZonesFromArgs(args, base.Bounds())

	variantCount := intParam(args, "variants", 1)
	if variantCount < 1 {
		variantCount = 1
	}
	allowVariants, _ := args["allow_variants"].(bool)
	if !allowVariants {
		variantCount = 1
	}
	if variantCount > 5 {
		variantCount = 5
	}

	writeAllowed := allowedWriteWithTeamWorkspace(ctx, nil)
	resolvedOutput, err := resolvePathWithAllowed(outputPath, workspace, effectiveRestrict(ctx, t.restrict), writeAllowed)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid output_path: %v", err))
	}
	if err := os.MkdirAll(filepath.Dir(resolvedOutput), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}

	outputs := make([]string, 0, variantCount)
	for i := 0; i < variantCount; i++ {
		canvas := cloneToRGBA(base)
		for layerIndex, layer := range layers {
			renderLayer(canvas, parsedFont, layer.withAutoLayout(i, layerIndex, canvas.Bounds()), avoidZones)
		}
		out := resolvedOutput
		if variantCount > 1 {
			out = variantPath(resolvedOutput, i+1)
		}
		f, err := os.Create(out)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to create output image: %v", err))
		}
		if err := png.Encode(f, canvas); err != nil {
			_ = f.Close()
			return ErrorResult(fmt.Sprintf("failed to encode output PNG: %v", err))
		}
		if err := f.Close(); err != nil {
			return ErrorResult(fmt.Sprintf("failed to close output PNG: %v", err))
		}
		outputs = append(outputs, out)
	}

	primary := outputs[0]
	forLLM := fmt.Sprintf("MEDIA:%s\nfont_path: %s\nfont_sha256: %s", primary, resolvedFont, fontSHA)
	if len(outputs) > 1 {
		forLLM += "\nvariants:"
		for _, p := range outputs {
			forLLM += "\n- " + p
		}
	}
	result := &Result{ForLLM: forLLM}
	result.Media = []bus.MediaFile{{Path: primary, MimeType: "image/png", Filename: filepath.Base(primary)}}
	result.Deliverable = fmt.Sprintf("[Rendered creative: %s]\nfont_sha256: %s", filepath.Base(primary), fontSHA)
	return result
}

func (t *RenderCreativeTool) loadBaseImage(ctx context.Context, args map[string]any, workspace string, allowed []string) (image.Image, error) {
	basePath, _ := args["base_image_path"].(string)
	if basePath != "" {
		resolved, err := resolveReadPathWithGlobalOverlay(ctx, basePath, workspace, effectiveRestrict(ctx, t.restrict), allowed)
		if err != nil {
			return nil, fmt.Errorf("invalid base_image_path: %v", err)
		}
		f, err := os.Open(resolved)
		if err != nil {
			return nil, fmt.Errorf("failed to open base_image_path: %v", err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base_image_path: %v", err)
		}
		return img, nil
	}
	w := intParam(args, "width", 1080)
	h := intParam(args, "height", 1080)
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("width/height must be between 1 and 8192")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	return img, nil
}

type renderTextLayer struct {
	Text        string
	X           int
	Y           int
	Size        float64
	Color       color.Color
	Align       string
	MaxWidth    int
	StrokeColor color.Color
	StrokeWidth int
	Layout      string
}

func parseRenderTextLayers(raw []any) []renderTextLayer {
	out := make([]renderTextLayer, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text, _ := m["text"].(string)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		layer := renderTextLayer{
			Text:        text,
			X:           intParam(m, "x", -1),
			Y:           intParam(m, "y", -1),
			Size:        floatParam(m, "size", 96),
			Color:       parseHexColor(stringParam(m, "color", "#FFFFFF")),
			Align:       stringParam(m, "align", "center"),
			MaxWidth:    intParam(m, "max_width", 0),
			StrokeColor: parseHexColor(stringParam(m, "stroke_color", "#000000")),
			StrokeWidth: intParam(m, "stroke_width", 0),
			Layout:      stringParam(m, "layout", ""),
		}
		out = append(out, layer)
	}
	return out
}

func (l renderTextLayer) withAutoLayout(variant, layerIndex int, bounds image.Rectangle) renderTextLayer {
	if l.MaxWidth <= 0 {
		l.MaxWidth = bounds.Dx() * 9 / 20
	}
	if l.X >= 0 && l.Y >= 0 && l.Layout != "auto" {
		return l
	}
	zones := []struct {
		x, y  int
		align string
	}{
		{bounds.Min.X + bounds.Dx()*8/100, bounds.Min.Y + bounds.Dy()*28/100, "left"},
		{bounds.Min.X + bounds.Dx()*92/100, bounds.Min.Y + bounds.Dy()*28/100, "right"},
		{bounds.Min.X + bounds.Dx()*8/100, bounds.Min.Y + bounds.Dy()*48/100, "left"},
		{bounds.Min.X + bounds.Dx()*8/100, bounds.Min.Y + bounds.Dy()*68/100, "left"},
		{bounds.Min.X + bounds.Dx()*92/100, bounds.Min.Y + bounds.Dy()*48/100, "right"},
	}
	z := zones[(variant+layerIndex)%len(zones)]
	if l.X < 0 || l.Layout == "auto" {
		l.X = z.x
	}
	if l.Y < 0 || l.Layout == "auto" {
		l.Y = z.y
	}
	if l.Align == "" || l.Layout == "auto" {
		l.Align = z.align
	}
	return l
}

func renderLayer(img *image.RGBA, fnt *opentype.Font, layer renderTextLayer, avoidZones []image.Rectangle) {
	layer = fitLayerToSafeBounds(fnt, layer, img.Bounds(), avoidZones)
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    layer.Size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return
	}

	lines := wrapText(layer.Text, face, layer.MaxWidth)
	lineHeight := int(layer.Size * 1.15)
	startY := layer.Y - (len(lines)-1)*lineHeight/2
	for i, line := range lines {
		width := font.MeasureString(face, line).Round()
		x := layer.X
		switch strings.ToLower(layer.Align) {
		case "right":
			x -= width
		case "center", "":
			x -= width / 2
		}
		y := startY + i*lineHeight
		if layer.StrokeWidth > 0 {
			drawTextStroke(img, face, line, x, y, layer.StrokeWidth, layer.StrokeColor)
		}
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(layer.Color),
			Face: face,
			Dot:  fixed.P(x, y),
		}
		d.DrawString(line)
	}
}

func fitLayerToSafeBounds(fnt *opentype.Font, layer renderTextLayer, bounds image.Rectangle, avoidZones []image.Rectangle) renderTextLayer {
	if layer.MaxWidth <= 0 {
		layer.MaxWidth = bounds.Dx() * 9 / 20
	}
	if layer.Size <= 0 {
		layer.Size = 96
	}
	marginX := max(12, bounds.Dx()*5/100)
	marginY := max(12, bounds.Dy()*5/100)

	for range 18 {
		box, ok := measureLayerBounds(fnt, layer)
		if !ok {
			return layer
		}
		if zone, ok := intersectingZone(box, avoidZones); ok {
			layer = moveLayerAwayFromZone(fnt, layer, bounds, zone, marginX, marginY)
			continue
		}
		if box.Min.X < bounds.Min.X+marginX || box.Max.X > bounds.Max.X-marginX ||
			box.Min.Y < bounds.Min.Y+marginY || box.Max.Y > bounds.Max.Y-marginY {
			if layer.Size > 28 {
				layer.Size *= 0.92
				continue
			}
			layer = shiftLayerInsideBounds(layer, box, bounds, marginX, marginY)
			return layer
		}
		return layer
	}
	box, ok := measureLayerBounds(fnt, layer)
	if ok {
		layer = shiftLayerInsideBounds(layer, box, bounds, marginX, marginY)
	}
	return layer
}

func moveLayerAwayFromZone(fnt *opentype.Font, layer renderTextLayer, bounds, zone image.Rectangle, marginX, marginY int) renderTextLayer {
	zoneCenterX := zone.Min.X + zone.Dx()/2
	if zoneCenterX <= bounds.Min.X+bounds.Dx()/2 {
		layer.Align = "left"
		layer.X = min(bounds.Max.X-marginX, zone.Max.X+marginX)
	} else {
		layer.Align = "right"
		layer.X = max(bounds.Min.X+marginX, zone.Min.X-marginX)
	}
	zoneCenterY := zone.Min.Y + zone.Dy()/2
	if zoneCenterY <= bounds.Min.Y+bounds.Dy()/2 {
		layer.Y = max(layer.Y, zone.Max.Y+marginY)
	} else {
		layer.Y = min(layer.Y, zone.Min.Y-marginY)
	}
	layer.MaxWidth = min(layer.MaxWidth, max(120, bounds.Dx()*42/100))
	box, ok := measureLayerBounds(fnt, layer)
	if ok {
		layer = shiftLayerInsideBounds(layer, box, bounds, marginX, marginY)
		box, ok = measureLayerBounds(fnt, layer)
		if ok && rectIntersects(box, zone) {
			layer = shiftLayerOutsideZone(layer, box, bounds, zone, marginY)
			if box, ok = measureLayerBounds(fnt, layer); ok {
				layer = shiftLayerInsideBounds(layer, box, bounds, marginX, marginY)
			}
		}
	}
	return layer
}

func shiftLayerOutsideZone(layer renderTextLayer, box, bounds, zone image.Rectangle, marginY int) renderTextLayer {
	zoneCenterY := zone.Min.Y + zone.Dy()/2
	if zoneCenterY <= bounds.Min.Y+bounds.Dy()/2 {
		layer.Y += zone.Max.Y + marginY - box.Min.Y
	} else {
		layer.Y -= box.Max.Y - (zone.Min.Y - marginY)
	}
	return layer
}

func intersectingZone(box image.Rectangle, zones []image.Rectangle) (image.Rectangle, bool) {
	for _, zone := range zones {
		if rectIntersects(box, zone) {
			return zone, true
		}
	}
	return image.Rectangle{}, false
}

func watermarkAvoidZonesFromArgs(args map[string]any, bounds image.Rectangle) []image.Rectangle {
	raw, ok := args["watermark"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	cfg := parseWatermarkConfig(raw)
	zones := watermarkAvoidZones(cfg, bounds)
	if len(zones) == 0 {
		return nil
	}
	return zones
}

func watermarkAvoidZones(cfg watermarkConfig, bounds image.Rectangle) []image.Rectangle {
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.Items) > 0 {
		var zones []image.Rectangle
		for _, item := range cfg.Items {
			zones = append(zones, watermarkAvoidZones(item, bounds)...)
		}
		return zones
	}
	shortSide := bounds.Dx()
	if bounds.Dy() < shortSide {
		shortSide = bounds.Dy()
	}
	size := int(float64(shortSide) * clampFloat(defaultFloat(cfg.ScalePct, 0.18), 0.04, 0.6))
	if size < 1 {
		size = 1
	}
	pad := max(12, shortSide*3/100)
	cx := bounds.Min.X + int(float64(bounds.Dx())*clampFloat(defaultFloat(cfg.XPct, 0.5), 0, 1))
	cy := bounds.Min.Y + int(float64(bounds.Dy())*clampFloat(defaultFloat(cfg.YPct, 0.12), 0, 1))
	zone := image.Rect(cx-size/2-pad, cy-size/2-pad, cx+size/2+pad, cy+size/2+pad).Intersect(bounds)
	if zone.Empty() {
		return nil
	}
	return []image.Rectangle{zone}
}

func shiftLayerInsideBounds(layer renderTextLayer, box image.Rectangle, bounds image.Rectangle, marginX, marginY int) renderTextLayer {
	minX := bounds.Min.X + marginX
	maxX := bounds.Max.X - marginX
	minY := bounds.Min.Y + marginY
	maxY := bounds.Max.Y - marginY
	if box.Min.X < minX {
		layer.X += minX - box.Min.X
	}
	if box.Max.X > maxX {
		layer.X -= box.Max.X - maxX
	}
	if box.Min.Y < minY {
		layer.Y += minY - box.Min.Y
	}
	if box.Max.Y > maxY {
		layer.Y -= box.Max.Y - maxY
	}
	return layer
}

func measureLayerBounds(fnt *opentype.Font, layer renderTextLayer) (image.Rectangle, bool) {
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    layer.Size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return image.Rectangle{}, false
	}
	lines := wrapText(layer.Text, face, layer.MaxWidth)
	if len(lines) == 0 {
		return image.Rectangle{}, false
	}
	lineHeight := int(layer.Size * 1.15)
	startY := layer.Y - (len(lines)-1)*lineHeight/2
	metrics := face.Metrics()
	ascent := metrics.Ascent.Round()
	descent := metrics.Descent.Round()
	minX := int(^uint(0) >> 1)
	maxX := -minX
	for _, line := range lines {
		width := font.MeasureString(face, line).Round()
		x := layer.X
		switch strings.ToLower(layer.Align) {
		case "right":
			x -= width
		case "center", "":
			x -= width / 2
		}
		minX = min(minX, x)
		maxX = max(maxX, x+width)
	}
	top := startY - ascent
	bottom := startY + (len(lines)-1)*lineHeight + descent
	stroke := max(layer.StrokeWidth, int(layer.Size*0.04))
	return image.Rect(minX-stroke, top-stroke, maxX+stroke, bottom+stroke), true
}

func rectIntersects(a, b image.Rectangle) bool {
	return a.Min.X < b.Max.X && a.Max.X > b.Min.X && a.Min.Y < b.Max.Y && a.Max.Y > b.Min.Y
}

func drawTextStroke(img *image.RGBA, face font.Face, text string, x, y, width int, col color.Color) {
	for dx := -width; dx <= width; dx++ {
		for dy := -width; dy <= width; dy++ {
			if dx == 0 && dy == 0 {
				continue
			}
			d := font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(col),
				Face: face,
				Dot:  fixed.P(x+dx, y+dy),
			}
			d.DrawString(text)
		}
	}
}

func wrapText(text string, face font.Face, maxWidth int) []string {
	if maxWidth <= 0 || font.MeasureString(face, text).Round() <= maxWidth {
		return strings.Split(text, "\n")
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			candidate := line + " " + word
			if font.MeasureString(face, candidate).Round() <= maxWidth {
				line = candidate
				continue
			}
			lines = append(lines, line)
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}

func cloneToRGBA(src image.Image) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

func variantPath(path string, n int) string {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("%s_v%d%s", stem, n, ext)
}

func parseHexColor(s string) color.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{255, 255, 255, 255}
	}
	v, err := hex.DecodeString(s)
	if err != nil {
		return color.RGBA{255, 255, 255, 255}
	}
	a := uint8(255)
	if len(v) == 4 {
		a = v[3]
	}
	return color.RGBA{v[0], v[1], v[2], a}
}

func intParam(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return fallback
	}
}

func floatParam(args map[string]any, key string, fallback float64) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return fallback
	}
}

func stringParam(args map[string]any, key string, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
