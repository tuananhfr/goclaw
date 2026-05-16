package http

import (
	"fmt"
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
	if _, err := io.Copy(out, file); err != nil {
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
