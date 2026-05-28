package tools

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type ApplyWatermarkTool struct {
	workspace       string
	restrict        bool
	allowedPrefixes []string
}

type watermarkConfig struct {
	Enabled  bool
	Items    []watermarkConfig
	Mode     string
	Text     string
	LogoPath string
	LogoURL  string
	XPct     float64
	YPct     float64
	ScalePct float64
	Opacity  float64
}

func NewApplyWatermarkTool(workspace string, restrict bool) *ApplyWatermarkTool {
	return &ApplyWatermarkTool{workspace: workspace, restrict: restrict}
}

func (t *ApplyWatermarkTool) Name() string { return "apply_watermark" }

func (t *ApplyWatermarkTool) Description() string {
	return "Apply a Facebook MCP-style watermark config to an image before previewing or posting. Use fb_get_watermark_config first, then call this only when has_watermark is true."
}

func (t *ApplyWatermarkTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}

func (t *ApplyWatermarkTool) AllowedPaths() []string {
	return append([]string(nil), t.allowedPrefixes...)
}

func (t *ApplyWatermarkTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"base_image_path": map[string]any{
				"type":        "string",
				"description": "Input image path. Supports workspace paths, absolute paths, MEDIA: paths, and /v1/files/... paths.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Optional output image path. Defaults to a sibling file named *-watermarked.jpg.",
			},
			"watermark": map[string]any{
				"type":        "object",
				"description": "Watermark config from fb_get_watermark_config. Shape is unchanged from Facebook MCP: enabled/items/mode/text/logo_path/logo_url/x_pct/y_pct/scale_pct/opacity.",
			},
		},
		"required": []string{"base_image_path", "watermark"},
	}
}

func (t *ApplyWatermarkTool) Execute(ctx context.Context, args map[string]any) *Result {
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	if workspace == "" {
		workspace = os.TempDir()
	}

	basePath, _ := args["base_image_path"].(string)
	if strings.TrimSpace(basePath) == "" {
		return ErrorResult("base_image_path is required")
	}

	rawCfg, ok := args["watermark"].(map[string]any)
	if !ok || rawCfg == nil {
		return ErrorResult("watermark must be an object")
	}
	cfg := parseWatermarkConfig(rawCfg)
	readAllowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	resolvedBase, err := t.resolveImagePath(ctx, basePath, workspace, readAllowed)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid base_image_path: %v", err))
	}

	hasWM, err := validateWatermarkConfig(cfg, false)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if !hasWM {
		return t.noWatermarkResult(ctx, args, workspace, resolvedBase)
	}

	base, err := loadImageFile(resolvedBase)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to load base_image_path: %v", err))
	}
	canvas := cloneToRGBA(base)
	if err := t.applyConfig(ctx, canvas, cfg, workspace, readAllowed); err != nil {
		return ErrorResult(err.Error())
	}

	outPath, err := t.resolveOutputPath(ctx, args, workspace, resolvedBase)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	mimeType, err := encodeWatermarkedImage(outPath, canvas)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to write watermarked image: %v", err))
	}

	forLLM := fmt.Sprintf("has_watermark: true\nMEDIA:%s\nUse this watermarked image for preview and Facebook posting.", outPath)
	result := &Result{ForLLM: forLLM}
	result.Media = []bus.MediaFile{{Path: outPath, MimeType: mimeType, Filename: filepath.Base(outPath)}}
	result.Deliverable = fmt.Sprintf("[Watermarked image: %s]", filepath.Base(outPath))
	return result
}

func (t *ApplyWatermarkTool) noWatermarkResult(ctx context.Context, args map[string]any, workspace, resolvedBase string) *Result {
	outRaw, _ := args["output_path"].(string)
	if strings.TrimSpace(outRaw) == "" {
		mimeType := imageMimeFromExt(resolvedBase)
		result := &Result{ForLLM: fmt.Sprintf("has_watermark: false\nMEDIA:%s\nNo enabled watermark config was found; use the original image.", resolvedBase)}
		result.Media = []bus.MediaFile{{Path: resolvedBase, MimeType: mimeType, Filename: filepath.Base(resolvedBase)}}
		return result
	}
	outPath, err := t.resolveOutputPath(ctx, args, workspace, resolvedBase)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	if err := copyFile(resolvedBase, outPath); err != nil {
		return ErrorResult(fmt.Sprintf("failed to copy unwatermarked image: %v", err))
	}
	mimeType := imageMimeFromExt(outPath)
	result := &Result{ForLLM: fmt.Sprintf("has_watermark: false\nMEDIA:%s\nNo enabled watermark config was found; copied original image.", outPath)}
	result.Media = []bus.MediaFile{{Path: outPath, MimeType: mimeType, Filename: filepath.Base(outPath)}}
	return result
}

func (t *ApplyWatermarkTool) applyConfig(ctx context.Context, canvas *image.RGBA, cfg watermarkConfig, workspace string, allowed []string) error {
	if len(cfg.Items) > 0 {
		applied := false
		for _, item := range cfg.Items {
			has, err := validateWatermarkConfig(item, true)
			if err != nil {
				return err
			}
			if !has {
				continue
			}
			if err := t.applySingle(ctx, canvas, item, workspace, allowed); err != nil {
				return err
			}
			applied = true
		}
		if !applied {
			return nil
		}
		return nil
	}
	return t.applySingle(ctx, canvas, cfg, workspace, allowed)
}

func (t *ApplyWatermarkTool) applySingle(ctx context.Context, canvas *image.RGBA, cfg watermarkConfig, workspace string, allowed []string) error {
	bounds := canvas.Bounds()
	shortSide := bounds.Dx()
	if bounds.Dy() < shortSide {
		shortSide = bounds.Dy()
	}
	wmWidth := int(float64(shortSide) * clampFloat(defaultFloat(cfg.ScalePct, 0.18), 0.04, 0.6))
	if wmWidth < 1 {
		wmWidth = 1
	}
	opacity := clampFloat(defaultFloat(cfg.Opacity, 0.45), 0.05, 1)

	var overlay *image.RGBA
	switch strings.ToLower(cfg.Mode) {
	case "text":
		overlay = renderWatermarkText(cfg.Text, wmWidth, opacity)
	case "logo", "":
		ref := cfg.LogoURL
		if ref == "" {
			ref = cfg.LogoPath
		}
		logo, err := t.loadLogo(ctx, ref, workspace, allowed)
		if err != nil {
			return err
		}
		overlay = resizeImage(trimTransparentPadding(cloneToRGBA(logo)), wmWidth)
		applyImageOpacity(overlay, opacity)
	default:
		return fmt.Errorf("unsupported watermark mode %q", cfg.Mode)
	}

	centerX := float64(bounds.Min.X) + float64(bounds.Dx())*clampFloat(defaultFloat(cfg.XPct, 0.5), 0, 1)
	centerY := float64(bounds.Min.Y) + float64(bounds.Dy())*clampFloat(defaultFloat(cfg.YPct, 0.12), 0, 1)
	left := int(clampFloat(centerX-float64(overlay.Bounds().Dx())/2, 0, float64(maxInt(0, bounds.Dx()-overlay.Bounds().Dx()))))
	top := int(clampFloat(centerY-float64(overlay.Bounds().Dy())/2, 0, float64(maxInt(0, bounds.Dy()-overlay.Bounds().Dy()))))
	draw.Draw(canvas, image.Rect(left, top, left+overlay.Bounds().Dx(), top+overlay.Bounds().Dy()), overlay, overlay.Bounds().Min, draw.Over)
	return nil
}

func (t *ApplyWatermarkTool) loadLogo(ctx context.Context, ref, workspace string, allowed []string) (image.Image, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("watermark logo requires logo_path or logo_url")
	}
	if isHTTPURL(ref) {
		return loadImageURL(ctx, ref)
	}
	resolved, err := t.resolveImagePath(ctx, ref, workspace, allowed)
	if err != nil {
		return nil, fmt.Errorf("invalid watermark logo path: %v", err)
	}
	return loadImageFile(resolved)
}

func (t *ApplyWatermarkTool) resolveImagePath(ctx context.Context, p, workspace string, allowed []string) (string, error) {
	p = normalizeMediaPath(p)
	resolved, err := resolveReadPathWithGlobalOverlay(ctx, p, workspace, effectiveRestrict(ctx, t.restrict), allowed)
	if err != nil {
		if fallback, ok, fallbackErr := resolveUploadedFileByBasename(ctx, workspace, p); fallbackErr != nil {
			return "", fallbackErr
		} else if ok {
			return fallback, nil
		}
		return "", err
	}
	if _, statErr := os.Stat(resolved); statErr == nil {
		return resolved, nil
	}
	if fallback, ok := resolveTeamRelativeFile(ctx, p); ok {
		return fallback, nil
	}
	if fallback, ok, fallbackErr := resolveUploadedFileByBasename(ctx, workspace, p); fallbackErr != nil {
		return "", fallbackErr
	} else if ok {
		return fallback, nil
	}
	if fallback, ok := resolveGeneratedImageByBasename(ctx, workspace, filepath.Base(p)); ok {
		return fallback, nil
	}
	return resolved, nil
}

func resolveTeamRelativeFile(ctx context.Context, p string) (string, bool) {
	if p == "" || filepath.IsAbs(p) {
		return "", false
	}
	rel := filepath.Clean(p)
	roots := []string{
		ToolTeamWorkspaceFromCtx(ctx),
		ToolTeamGlobalWorkspaceFromCtx(ctx),
		ToolTeamRootFromCtx(ctx),
	}
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if seen[root] {
			continue
		}
		seen[root] = true
		candidate := filepath.Join(root, rel)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func resolveGeneratedImageByBasename(ctx context.Context, workspace, name string) (string, bool) {
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", false
	}
	roots := []string{
		workspace,
		ToolTeamWorkspaceFromCtx(ctx),
		ToolTeamGlobalWorkspaceFromCtx(ctx),
		ToolTeamRootFromCtx(ctx),
	}
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if seen[root] {
			continue
		}
		seen[root] = true

		if candidate, ok := newestGeneratedFile(root, name); ok {
			return candidate, true
		}
	}
	return "", false
}

func newestGeneratedFile(root, name string) (string, bool) {
	patterns := []string{
		filepath.Join(root, "generated", "*", name),
		filepath.Join(root, "generated", name),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			continue
		}
		for i := len(matches) - 1; i >= 0; i-- {
			if info, statErr := os.Stat(matches[i]); statErr == nil && !info.IsDir() {
				return matches[i], true
			}
		}
	}
	return "", false
}

func (t *ApplyWatermarkTool) resolveOutputPath(ctx context.Context, args map[string]any, workspace, resolvedBase string) (string, error) {
	outRaw, _ := args["output_path"].(string)
	if strings.TrimSpace(outRaw) == "" {
		outRaw = strings.TrimSuffix(resolvedBase, filepath.Ext(resolvedBase)) + "-watermarked.jpg"
	}
	writeAllowed := allowedWriteWithTeamWorkspace(ctx, nil)
	out, err := resolvePathWithAllowed(normalizeMediaPath(outRaw), workspace, effectiveRestrict(ctx, t.restrict), writeAllowed)
	if err != nil {
		return "", fmt.Errorf("invalid output_path: %v", err)
	}
	return out, nil
}

func parseWatermarkConfig(m map[string]any) watermarkConfig {
	cfg := watermarkConfig{
		Enabled:  boolParam(m, "enabled", false),
		Mode:     stringParam(m, "mode", ""),
		Text:     stringParam(m, "text", ""),
		LogoPath: stringParam(m, "logo_path", ""),
		LogoURL:  stringParam(m, "logo_url", ""),
		XPct:     normalizedPercentParam(m, "x_pct", -1),
		YPct:     normalizedPercentParam(m, "y_pct", -1),
		ScalePct: normalizedPercentParam(m, "scale_pct", -1),
		Opacity:  normalizedPercentParam(m, "opacity", -1),
	}
	if rawItems, ok := m["items"].([]any); ok {
		for _, raw := range rawItems {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			cfg.Items = append(cfg.Items, parseWatermarkConfig(item))
		}
	}
	return cfg
}

func validateWatermarkConfig(cfg watermarkConfig, item bool) (bool, error) {
	if !cfg.Enabled {
		return false, nil
	}
	if !item && len(cfg.Items) > 0 {
		anyValid := false
		for _, child := range cfg.Items {
			has, err := validateWatermarkConfig(child, true)
			if err != nil {
				return false, err
			}
			anyValid = anyValid || has
		}
		return anyValid, nil
	}
	switch strings.ToLower(cfg.Mode) {
	case "logo", "":
		if strings.TrimSpace(cfg.LogoPath) == "" && strings.TrimSpace(cfg.LogoURL) == "" {
			return false, fmt.Errorf("watermark mode logo requires logo_path or logo_url")
		}
		return true, nil
	case "text":
		if strings.TrimSpace(cfg.Text) == "" {
			return false, fmt.Errorf("watermark mode text requires non-empty text")
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported watermark mode %q", cfg.Mode)
	}
}

func normalizeMediaPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "MEDIA:")
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err == nil {
			p = u.Path
			if u.Host != "" {
				p = "//" + u.Host + p
			}
		} else {
			p = strings.TrimPrefix(p, "file://")
		}
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
	}
	if strings.HasPrefix(p, "/v1/files/") {
		p = strings.TrimPrefix(p, "/v1/files/")
		if idx := strings.IndexAny(p, "?#"); idx >= 0 {
			p = p[:idx]
		}
		if decoded, err := url.PathUnescape(p); err == nil {
			p = decoded
		}
		if len(p) >= 2 && p[1] == ':' {
			return filepath.FromSlash(p)
		}
		p = "/" + strings.TrimPrefix(p, "/")
	}
	return filepath.FromSlash(p)
}

func normalizedPercentParam(args map[string]any, key string, fallback float64) float64 {
	v := floatParam(args, key, fallback)
	if v > 1 {
		return v / 100
	}
	return v
}

func defaultFloat(v, fallback float64) float64 {
	if v < 0 {
		return fallback
	}
	return v
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://")
}

func loadImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func loadImageURL(ctx context.Context, rawURL string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to fetch logo_url: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func trimTransparentPadding(img *image.RGBA) *image.RGBA {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y).A <= 8 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return img
	}
	rect := image.Rect(0, 0, maxX-minX+1, maxY-minY+1)
	out := image.NewRGBA(rect)
	draw.Draw(out, rect, img, image.Point{X: minX, Y: minY}, draw.Src)
	return out
}

func resizeImage(img image.Image, width int) *image.RGBA {
	b := img.Bounds()
	if width <= 0 || b.Dx() <= 0 || b.Dy() <= 0 {
		return cloneToRGBA(img)
	}
	height := maxInt(1, int(float64(b.Dy())*float64(width)/float64(b.Dx())))
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

func applyImageOpacity(img *image.RGBA, opacity float64) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			c.A = uint8(clampFloat(float64(c.A)*opacity, 0, 255))
			img.SetRGBA(x, y, c)
		}
	}
}

func renderWatermarkText(text string, width int, opacity float64) *image.RGBA {
	fontSize := maxInt(16, int(float64(width)*0.22))
	scale := maxInt(1, fontSize/13)
	smallW := maxInt(1, width/scale)
	smallH := maxInt(1, int(float64(fontSize)*1.8)/scale)
	small := image.NewRGBA(image.Rect(0, 0, smallW, smallH))
	bgAlpha := uint8(clampFloat(255*opacity*0.35, 0, 255))
	draw.Draw(small, small.Bounds(), &image.Uniform{C: color.RGBA{0, 0, 0, bgAlpha}}, image.Point{}, draw.Src)

	face := basicfont.Face7x13
	textWidth := font.MeasureString(face, text).Round()
	x := (smallW - textWidth) / 2
	if x < 0 {
		x = 0
	}
	y := smallH/2 + 5
	d := font.Drawer{
		Dst:  small,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, uint8(clampFloat(255*opacity, 0, 255))}),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)

	dst := image.NewRGBA(image.Rect(0, 0, width, maxInt(1, smallH*scale)))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), small, small.Bounds(), draw.Over, nil)
	return dst
}

func encodeWatermarkedImage(path string, img image.Image) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", png.Encode(f, img)
	default:
		return "image/jpeg", jpeg.Encode(f, img, &jpeg.Options{Quality: 92})
	}
}

func imageMimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}
