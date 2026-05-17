package http

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const maxMCPWatermarkAssetSize int64 = 10 * 1024 * 1024

func normalizeWatermarkAsset(r io.Reader, ext string) ([]byte, string, string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, ext, "", err
	}
	if strings.ToLower(ext) != ".png" {
		return raw, ext, "", nil
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ext, "", err
	}
	bounds := img.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X-1, bounds.Min.Y-1
	const alphaThreshold uint32 = 0x0800

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a <= alphaThreshold {
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
		return raw, ext, "image/png", nil
	}
	cropRect := image.Rect(0, 0, maxX-minX+1, maxY-minY+1)
	if minX == bounds.Min.X && minY == bounds.Min.Y && cropRect.Dx() == bounds.Dx() && cropRect.Dy() == bounds.Dy() {
		return raw, ext, "image/png", nil
	}

	cropped := image.NewNRGBA(cropRect)
	draw.Draw(cropped, cropRect, img, image.Point{X: minX, Y: minY}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, cropped); err != nil {
		return nil, ext, "", err
	}
	return out.Bytes(), ".png", "image/png", nil
}

func (h *MCPHandler) handleUploadWatermarkAsset(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if h.dataDir == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mcp asset storage is not configured"})
		return
	}
	serverID := r.PathValue("id")
	if serverID != "" {
		parsed, err := uuid.Parse(serverID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
			return
		}
		if _, err := h.store.GetServer(r.Context(), parsed); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "server", parsed.String())})
			return
		}
	} else {
		serverID = "unassigned"
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMCPWatermarkAssetSize)
	if err := r.ParseMultipartForm(maxMCPWatermarkAssetSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	origName := filepath.Base(header.Filename)
	if origName == "." || origName == "/" || strings.Contains(origName, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}
	mimeType := media.DetectMIMEType(origName)
	if !strings.HasPrefix(mimeType, "image/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "watermark asset must be an image"})
		return
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if ext == "" {
		ext = ".png"
	}
	assetBytes, normalizedExt, normalizedMime, err := normalizeWatermarkAsset(file, ext)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to process watermark asset: %v", err)})
		return
	}
	if normalizedExt != "" {
		ext = normalizedExt
	}
	if normalizedMime != "" {
		mimeType = normalizedMime
	}

	tenantDirName := store.TenantIDFromContext(r.Context()).String()
	if slug := store.TenantSlugFromContext(r.Context()); slug != "" {
		tenantDirName = slug
	}
	if tenantDirName == uuid.Nil.String() || tenantDirName == "" {
		tenantDirName = store.MasterTenantID.String()
	}
	assetDir := filepath.Join(config.ExpandHome(h.dataDir), "mcp-assets", tenantDirName, serverID, "watermarks")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create asset directory: %v", err)})
		return
	}
	if fi, err := os.Lstat(assetDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "asset directory is invalid"})
		return
	}

	filename := fmt.Sprintf("wm_%d_%s%s", time.Now().UnixNano(), uuid.NewString(), ext)
	dstPath := filepath.Join(assetDir, filename)
	cleanDst := filepath.Clean(dstPath)
	cleanDir := filepath.Clean(assetDir)
	sep := string(filepath.Separator)
	if !strings.HasPrefix(cleanDst, cleanDir+sep) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid asset path"})
		return
	}

	out, err := os.OpenFile(cleanDst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create asset: %v", err)})
		return
	}
	defer out.Close()
	if _, err := out.Write(assetBytes); err != nil {
		_ = os.Remove(cleanDst)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save asset: %v", err)})
		return
	}

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.ToSlash(filepath.Clean(cleanDst)), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)
	writeJSON(w, http.StatusOK, map[string]string{
		"path":      cleanDst,
		"url_path":  urlPath,
		"url":       urlPath + "?ft=" + ft,
		"mime_type": mimeType,
		"filename":  origName,
	})
}
