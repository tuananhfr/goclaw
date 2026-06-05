package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	googleDriveVisualMaxImageBytes  = 10 * 1024 * 1024
	googleDriveVisualPreviewMaxSide = 1600
)

type googleDriveMCPSettings struct {
	GoogleDrive *struct {
		RootFolderID           string `json:"root_folder_id"`
		RootFolderName         string `json:"root_folder_name"`
		CacheDir               string `json:"cache_dir"`
		VisualIndexEnabled     *bool  `json:"visual_index_enabled"`
		VisualIndexProvider    string `json:"visual_index_provider"`
		VisualIndexModel       string `json:"visual_index_model"`
		VisualFormatProvider   string `json:"visual_format_provider"`
		VisualFormatModel      string `json:"visual_format_model"`
		VisualIndexConcurrency int    `json:"visual_index_concurrency"`
		VisualIndexMaxPerRun   *int   `json:"visual_index_max_per_run"`
		VisualIndexTime        string `json:"visual_index_time"`
	} `json:"google_drive"`
}

type googleDriveFolderIndex struct {
	RootFolderID      string                       `json:"root_folder_id"`
	Files             map[string]googleDriveFile   `json:"files"`
	Folders           map[string]googleDriveFolder `json:"folders"`
	PublicImports     map[string]googleDriveFile   `json:"public_imports"`
	Status            googleDriveSyncStatus        `json:"status"`
	VisualIndexStatus googleDriveVisualIndexStatus `json:"visual_index_status"`
}

type googleDriveSyncStatus struct {
	Syncing         bool     `json:"syncing"`
	IndexedFiles    int      `json:"indexed_files"`
	DownloadedFiles int      `json:"downloaded_files"`
	TrashedFiles    int      `json:"trashed_files"`
	Errors          []string `json:"errors"`
}

type googleDriveFolder struct {
	DriveFileID  string   `json:"drive_file_id"`
	Name         string   `json:"name"`
	Parents      []string `json:"parents"`
	ModifiedTime string   `json:"modified_time,omitempty"`
	WebViewLink  string   `json:"web_view_link,omitempty"`
	Trashed      bool     `json:"trashed,omitempty"`
}

type googleDriveFile struct {
	DriveFileID             string   `json:"drive_file_id"`
	Name                    string   `json:"name"`
	MimeType                string   `json:"mime_type"`
	Parents                 []string `json:"parents"`
	ModifiedTime            string   `json:"modified_time,omitempty"`
	MD5Checksum             string   `json:"md5_checksum,omitempty"`
	WebViewLink             string   `json:"web_view_link,omitempty"`
	Size                    int64    `json:"size,omitempty"`
	LocalPath               string   `json:"local_path,omitempty"`
	Media                   string   `json:"media,omitempty"`
	Trashed                 bool     `json:"trashed,omitempty"`
	Version                 string   `json:"version,omitempty"`
	SyncedAt                string   `json:"synced_at,omitempty"`
	VisualSummaryVI         string   `json:"visual_summary_vi,omitempty"`
	VisualDescriptionVI     string   `json:"visual_description_vi,omitempty"`
	VisualTagsVI            []string `json:"visual_tags_vi,omitempty"`
	VisualTagsEN            []string `json:"visual_tags_en,omitempty"`
	VisualMainSubject       string   `json:"visual_main_subject,omitempty"`
	VisualSceneType         string   `json:"visual_scene_type,omitempty"`
	VisualDetectedText      []string `json:"visual_detected_text,omitempty"`
	VisualUsableAsReference *bool    `json:"visual_usable_as_reference,omitempty"`
	VisualQuality           string   `json:"visual_quality,omitempty"`
	VisualIndexedAt         string   `json:"visual_indexed_at,omitempty"`
	VisualIndexVersion      string   `json:"visual_index_version,omitempty"`
}

type googleDriveVisualIndexStatus struct {
	Indexing      bool     `json:"indexing"`
	IndexedImages int      `json:"indexed_images"`
	PendingImages int      `json:"pending_images"`
	FailedImages  int      `json:"failed_images"`
	LastIndexedAt string   `json:"last_indexed_at,omitempty"`
	Errors        []string `json:"errors"`
}

type googleDriveFolderOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Parent string `json:"parent,omitempty"`
}

func (h *MCPHandler) handleGoogleDriveFolders(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}

	srv, err := h.store.GetServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}

	rootFolderID, cacheDir := googleDriveCacheConfig(srv.Settings, srv.Headers)
	if rootFolderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.root_folder_id is not configured"})
		return
	}
	if cacheDir == "" {
		cacheDir = "/app/workspace/drive-cache"
	}
	rootName := googleDriveRootName(srv.Settings)
	if rootName == "" {
		rootName = "Root folder"
	}
	rootFallback := []googleDriveFolderOption{{
		ID:   rootFolderID,
		Name: rootName,
		Path: rootName,
	}}

	indexPath := filepath.Join(cacheDir, safeDriveCacheSegment(rootFolderID), "index.json")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"folders": rootFallback,
			"status":  "index_not_ready",
			"sync":    googleDriveSyncStatus{},
		})
		return
	}

	var index googleDriveFolderIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid Google Drive index"})
		return
	}
	if index.RootFolderID == "" {
		index.RootFolderID = rootFolderID
	}
	if index.VisualIndexStatus.Indexing && !h.isGoogleDriveVisualIndexActive(cacheDir, rootFolderID) {
		index.VisualIndexStatus.Indexing = false
		index.VisualIndexStatus.PendingImages = 0
		index.VisualIndexStatus.Errors = append(index.VisualIndexStatus.Errors, "indexing status cleared after server restart")
		index.VisualIndexStatus.Errors = tailStrings(index.VisualIndexStatus.Errors, 20)
		if err := writeGoogleDriveIndex(indexPath, index); err != nil {
			slog.Warn("gdrive.visual_index.clear_stale_failed", "root", rootFolderID, "error", err)
		} else {
			slog.Info("gdrive.visual_index.clear_stale", "root", rootFolderID)
		}
	}

	folders := make([]googleDriveFolderOption, 0, len(index.Folders)+1)
	if f, ok := index.Folders[index.RootFolderID]; ok && f.Name != "" {
		rootName = f.Name
	}
	folders = append(folders, googleDriveFolderOption{ID: index.RootFolderID, Name: rootName, Path: rootName})
	for id, folder := range index.Folders {
		if id == index.RootFolderID || folder.Trashed {
			continue
		}
		parent := ""
		if len(folder.Parents) > 0 {
			parent = folder.Parents[0]
		}
		folders = append(folders, googleDriveFolderOption{
			ID:     id,
			Name:   folder.Name,
			Path:   googleDriveFolderPath(id, index.RootFolderID, index.Folders),
			Parent: parent,
		})
	}

	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Path) < strings.ToLower(folders[j].Path)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"folders":      folders,
		"status":       "ok",
		"sync":         index.Status,
		"visual_index": index.VisualIndexStatus,
	})
}

func (h *MCPHandler) handleGoogleDriveSyncStart(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}

	srv, err := h.store.GetServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}

	rootFolderID, _ := googleDriveCacheConfig(srv.Settings, srv.Headers)
	if rootFolderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.root_folder_id is not configured"})
		return
	}

	var args []string
	_ = json.Unmarshal(srv.Args, &args)
	headers := map[string]string{}
	_ = json.Unmarshal(srv.Headers, &headers)
	env := map[string]string{}
	_ = json.Unmarshal(srv.Env, &env)

	result, err := mcpbridge.CallTemporaryTool(r.Context(), srv.Transport, srv.Command, args, env, srv.URL, headers, "gdrive_sync_now", map[string]any{"scope": "changes"})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if h.poolEvictor != nil {
		tid := store.TenantIDFromContext(r.Context())
		h.poolEvictor.Evict(tid, srv.Name)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "changes_synced",
		"result": result,
	})
}

func (h *MCPHandler) handleGoogleDriveIndexImages(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}
	if h.providerReg == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider registry is not configured"})
		return
	}

	srv, err := h.store.GetServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}

	rootFolderID, cacheDir := googleDriveCacheConfig(srv.Settings, srv.Headers)
	if rootFolderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.root_folder_id is not configured"})
		return
	}
	if cacheDir == "" {
		cacheDir = "/app/workspace/drive-cache"
	}
	visualCfg := parseGoogleDriveVisualConfig(srv.Settings)
	if !visualCfg.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.visual_index_enabled is disabled"})
		return
	}
	if visualCfg.Provider == "" || visualCfg.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.visual_index_provider and visual_index_model are required"})
		return
	}
	if (visualCfg.FormatProvider == "") != (visualCfg.FormatModel == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "google_drive.visual_format_provider and visual_format_model must be configured together"})
		return
	}

	var req struct {
		FolderIDOrURL string `json:"folder_id_or_url"`
		Limit         int    `json:"limit"`
		Force         bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Limit < 0 || (visualCfg.MaxPerRun > 0 && (req.Limit == 0 || req.Limit > visualCfg.MaxPerRun)) {
		req.Limit = visualCfg.MaxPerRun
	}

	if _, err := h.providerReg.GetForTenant(store.TenantIDFromContext(r.Context()), visualCfg.Provider); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if visualCfg.HasFormatter() {
		if _, err := h.providerReg.GetForTenant(store.TenantIDFromContext(r.Context()), visualCfg.FormatProvider); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	jobKey := googleDriveVisualJobKey(cacheDir, rootFolderID)
	if _, loaded := h.visualIndexJobs.LoadOrStore(jobKey, true); loaded {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "images_index_already_running",
		})
		return
	}

	ctx := context.WithoutCancel(r.Context())
	slog.Info("gdrive.visual_index.requested",
		"server", srv.Name,
		"root", rootFolderID,
		"provider", visualCfg.Provider,
		"model", visualCfg.Model,
		"format_provider", visualCfg.FormatProvider,
		"format_model", visualCfg.FormatModel,
		"limit", req.Limit,
		"force", req.Force,
	)
	go func() {
		defer h.visualIndexJobs.Delete(jobKey)
		result, err := h.indexGoogleDriveImages(ctx, cacheDir, rootFolderID, req.FolderIDOrURL, req.Limit, req.Force, visualCfg)
		if err != nil {
			slog.Error("gdrive.visual_index.failed", "server", srv.Name, "root", rootFolderID, "error", err)
			return
		}
		slog.Info("gdrive.visual_index.completed",
			"server", srv.Name,
			"root", rootFolderID,
			"indexed", result.IndexedImages,
			"pending", result.PendingImages,
			"failed", result.FailedImages,
		)
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "images_index_started",
	})
}

func googleDriveVisualJobKey(cacheDir, rootFolderID string) string {
	return filepath.Clean(filepath.Join(cacheDir, safeDriveCacheSegment(rootFolderID)))
}

func (h *MCPHandler) isGoogleDriveVisualIndexActive(cacheDir, rootFolderID string) bool {
	_, ok := h.visualIndexJobs.Load(googleDriveVisualJobKey(cacheDir, rootFolderID))
	return ok
}

func googleDriveCacheConfig(settingsRaw, headersRaw json.RawMessage) (string, string) {
	var settings googleDriveMCPSettings
	_ = json.Unmarshal(settingsRaw, &settings)
	rootFolderID := ""
	cacheDir := ""
	if settings.GoogleDrive != nil {
		rootFolderID = strings.TrimSpace(settings.GoogleDrive.RootFolderID)
		cacheDir = strings.TrimSpace(settings.GoogleDrive.CacheDir)
	}

	var headers map[string]string
	_ = json.Unmarshal(headersRaw, &headers)
	if rootFolderID == "" {
		rootFolderID = strings.TrimSpace(headers["x-gdrive-root-folder-id"])
	}
	if cacheDir == "" {
		cacheDir = strings.TrimSpace(headers["x-gdrive-cache-dir"])
	}
	return rootFolderID, cacheDir
}

func googleDriveRootName(settingsRaw json.RawMessage) string {
	var raw struct {
		GoogleDrive *struct {
			RootFolderName string `json:"root_folder_name"`
		} `json:"google_drive"`
	}
	_ = json.Unmarshal(settingsRaw, &raw)
	if raw.GoogleDrive == nil {
		return ""
	}
	return strings.TrimSpace(raw.GoogleDrive.RootFolderName)
}

type googleDriveVisualConfig struct {
	Enabled        bool
	Provider       string
	Model          string
	FormatProvider string
	FormatModel    string
	Concurrency    int
	MaxPerRun      int
}

func googleDriveVisualConfigFromDefaults() googleDriveVisualConfig {
	return googleDriveVisualConfig{Enabled: true, Concurrency: 1, MaxPerRun: 100}
}

func (cfg googleDriveVisualConfig) HasFormatter() bool {
	return cfg.FormatProvider != "" && cfg.FormatModel != ""
}

func parseGoogleDriveVisualConfig(settingsRaw json.RawMessage) googleDriveVisualConfig {
	cfg := googleDriveVisualConfigFromDefaults()
	var settings googleDriveMCPSettings
	_ = json.Unmarshal(settingsRaw, &settings)
	if settings.GoogleDrive == nil {
		return cfg
	}
	if settings.GoogleDrive.VisualIndexEnabled != nil {
		cfg.Enabled = *settings.GoogleDrive.VisualIndexEnabled
	}
	cfg.Provider = strings.TrimSpace(settings.GoogleDrive.VisualIndexProvider)
	cfg.Model = strings.TrimSpace(settings.GoogleDrive.VisualIndexModel)
	cfg.FormatProvider = strings.TrimSpace(settings.GoogleDrive.VisualFormatProvider)
	cfg.FormatModel = strings.TrimSpace(settings.GoogleDrive.VisualFormatModel)
	if settings.GoogleDrive.VisualIndexConcurrency > 0 {
		cfg.Concurrency = settings.GoogleDrive.VisualIndexConcurrency
	}
	if settings.GoogleDrive.VisualIndexMaxPerRun != nil {
		cfg.MaxPerRun = max(*settings.GoogleDrive.VisualIndexMaxPerRun, 0)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.MaxPerRun < 0 {
		cfg.MaxPerRun = 100
	}
	return cfg
}

type googleDriveVisualIndexResult struct {
	Changed       bool     `json:"changed"`
	IndexedImages int      `json:"indexed_images"`
	PendingImages int      `json:"pending_images"`
	FailedImages  int      `json:"failed_images"`
	SkippedImages int      `json:"skipped_images"`
	Errors        []string `json:"errors"`
}

func (h *MCPHandler) indexGoogleDriveImages(ctx context.Context, cacheDir, rootFolderID, folderIDOrURL string, limit int, force bool, cfg googleDriveVisualConfig) (googleDriveVisualIndexResult, error) {
	indexPath := filepath.Join(cacheDir, safeDriveCacheSegment(rootFolderID), "index.json")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return googleDriveVisualIndexResult{}, fmt.Errorf("read Google Drive index: %w", err)
	}
	var index googleDriveFolderIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return googleDriveVisualIndexResult{}, fmt.Errorf("invalid Google Drive index: %w", err)
	}
	if index.Files == nil {
		index.Files = map[string]googleDriveFile{}
	}
	if index.Folders == nil {
		index.Folders = map[string]googleDriveFolder{}
	}

	folderID := parseGoogleDriveID(folderIDOrURL)
	cacheRoot := filepath.Clean(filepath.Join(cacheDir, safeDriveCacheSegment(rootFolderID)))
	candidates := make([]googleDriveFile, 0)
	for _, file := range index.Files {
		if file.Trashed || file.LocalPath == "" || !strings.HasPrefix(file.MimeType, "image/") {
			continue
		}
		if folderID != "" && !googleDriveItemUnderFolder(file.Parents, folderID, true, index.Folders) {
			continue
		}
		if !force && file.VisualIndexVersion == file.Version {
			continue
		}
		local := filepath.Clean(file.LocalPath)
		if local != cacheRoot && !strings.HasPrefix(local, cacheRoot+string(os.PathSeparator)) {
			continue
		}
		candidates = append(candidates, file)
	}
	if cfg.MaxPerRun > 0 && (limit <= 0 || limit > cfg.MaxPerRun) {
		limit = cfg.MaxPerRun
	}
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	tenantID := store.TenantIDFromContext(ctx)
	provider, err := h.providerReg.GetForTenant(tenantID, cfg.Provider)
	if err != nil {
		return googleDriveVisualIndexResult{}, fmt.Errorf("resolve visual index provider: %w", err)
	}
	var formatter providers.Provider
	if cfg.HasFormatter() {
		formatter, err = h.providerReg.GetForTenant(tenantID, cfg.FormatProvider)
		if err != nil {
			return googleDriveVisualIndexResult{}, fmt.Errorf("resolve visual format provider: %w", err)
		}
	}

	index.VisualIndexStatus = googleDriveVisualIndexStatus{
		Indexing:      true,
		IndexedImages: 0,
		PendingImages: len(candidates),
		FailedImages:  0,
		Errors:        []string{},
	}
	if err := writeGoogleDriveIndex(indexPath, index); err != nil {
		return googleDriveVisualIndexResult{}, err
	}

	var mu sync.Mutex
	cursor := 0
	indexed := 0
	failed := 0
	errors := []string{}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(candidates) && len(candidates) > 0 {
		concurrency = len(candidates)
	}
	if concurrency == 0 {
		concurrency = 1
	}

	worker := func() {
		for {
			mu.Lock()
			if cursor >= len(candidates) {
				mu.Unlock()
				return
			}
			file := candidates[cursor]
			cursor++
			mu.Unlock()

			visual, callErr := h.callVisualIndexProvider(ctx, provider, cfg.Model, formatter, cfg.FormatModel, file)

			mu.Lock()
			if callErr != nil {
				failed++
				errors = append(errors, fmt.Sprintf("%s: %v", file.Name, callErr))
			} else if current, ok := index.Files[file.DriveFileID]; ok {
				applyGoogleDriveVisual(&current, visual)
				current.VisualIndexedAt = time.Now().UTC().Format(time.RFC3339)
				current.VisualIndexVersion = current.Version
				index.Files[file.DriveFileID] = current
				indexed++
			}
			index.VisualIndexStatus = googleDriveVisualIndexStatus{
				Indexing:      true,
				IndexedImages: indexed,
				PendingImages: max(len(candidates)-indexed-failed, 0),
				FailedImages:  failed,
				LastIndexedAt: time.Now().UTC().Format(time.RFC3339),
				Errors:        tailStrings(errors, 20),
			}
			_ = writeGoogleDriveIndex(indexPath, index)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker()
		}()
	}
	wg.Wait()

	index.VisualIndexStatus = googleDriveVisualIndexStatus{
		Indexing:      false,
		IndexedImages: indexed,
		PendingImages: max(len(candidates)-indexed-failed, 0),
		FailedImages:  failed,
		LastIndexedAt: time.Now().UTC().Format(time.RFC3339),
		Errors:        tailStrings(errors, 20),
	}
	if err := writeGoogleDriveIndex(indexPath, index); err != nil {
		return googleDriveVisualIndexResult{}, err
	}
	return googleDriveVisualIndexResult{
		Changed:       indexed > 0,
		IndexedImages: indexed,
		PendingImages: index.VisualIndexStatus.PendingImages,
		FailedImages:  failed,
		SkippedImages: len(index.Files) - len(candidates),
		Errors:        tailStrings(errors, 20),
	}, nil
}

func googleDriveFolderPath(id, rootID string, folders map[string]googleDriveFolder) string {
	names := []string{}
	seen := map[string]bool{}
	for current := id; current != "" && !seen[current]; {
		seen[current] = true
		folder, ok := folders[current]
		if !ok {
			break
		}
		if folder.Name != "" {
			names = append([]string{folder.Name}, names...)
		}
		if current == rootID || len(folder.Parents) == 0 {
			break
		}
		current = folder.Parents[0]
	}
	if len(names) == 0 {
		return id
	}
	return strings.Join(names, " / ")
}

type googleDriveVisualPayload struct {
	SummaryVI         string   `json:"summary_vi"`
	DescriptionVI     string   `json:"description_vi"`
	TagsVI            []string `json:"tags_vi"`
	TagsEN            []string `json:"tags_en"`
	MainSubject       string   `json:"main_subject"`
	SceneType         string   `json:"scene_type"`
	DetectedText      []string `json:"detected_text"`
	UsableAsReference bool     `json:"usable_as_reference"`
	Quality           string   `json:"quality"`
}

const googleDriveVisualPrompt = `Phan tich anh de lap chi muc tim kiem noi bo.
Tra ve JSON hop le, khong markdown, khong giai thich.

Yeu cau:
- Chi mo ta nhung gi nhin thay duoc.
- Khong bia thuong hieu, thong so, vat the neu khong chac.
- Neu anh mo hoac khong chac, ghi than trong trong description_vi.
- Mo ta chi tiet de tim kiem tot, nhung summary_vi phai ngan gon.

Schema:
{
  "summary_vi": "1 cau ngan mo ta anh",
  "description_vi": "3-6 cau mo ta chi tiet boi canh, chu the, vat the, mau sac, chat lieu, goc chup, ung dung neu thay ro",
  "tags_vi": ["8-15 tag tieng Viet"],
  "tags_en": ["8-15 English tags"],
  "main_subject": "chu the chinh",
  "scene_type": "product|factory|construction|food|document|people|other",
  "detected_text": ["chu nhin thay neu co"],
  "usable_as_reference": true,
  "quality": "low|medium|high"
}`

const googleDriveVisualCaptionPrompt = `Describe this image for an internal searchable asset index.
Mention only visible objects, scene, colors, materials, composition, and readable text.
Return plain text only. No JSON. No markdown.`

const googleDriveVisualRetryPrompt = `Return one compact valid JSON object only. No markdown. No explanation. No thinking text.
Use short values to avoid truncation.
Schema keys exactly:
{"summary_vi":"","description_vi":"","tags_vi":[],"tags_en":[],"main_subject":"","scene_type":"product|factory|construction|food|document|people|other","detected_text":[],"usable_as_reference":true,"quality":"low|medium|high"}`

const googleDriveVisualFormatPrompt = `Convert this image caption into one compact valid JSON object for an internal image search index.
Return JSON only. No markdown. No explanation.

Rules:
- Only use facts present in the caption.
- If uncertain, keep fields generic.
- Write summary_vi and description_vi in Vietnamese.
- Use empty arrays when tags or text are unclear.

Schema keys exactly:
{"summary_vi":"","description_vi":"","tags_vi":[],"tags_en":[],"main_subject":"","scene_type":"product|factory|construction|food|document|people|other","detected_text":[],"usable_as_reference":true,"quality":"low|medium|high"}

Caption:
`

func (h *MCPHandler) callVisualIndexProvider(ctx context.Context, provider providers.Provider, model string, formatter providers.Provider, formatterModel string, file googleDriveFile) (googleDriveVisualPayload, error) {
	data, mime, err := prepareGoogleDriveVisualImage(file.LocalPath)
	if err != nil {
		return googleDriveVisualPayload{}, err
	}

	prompt := googleDriveVisualPrompt
	maxTokens := 2400
	if formatter != nil && formatterModel != "" {
		prompt = googleDriveVisualCaptionPrompt
		maxTokens = 700
	}
	raw, err := h.callVisualIndexProviderRaw(ctx, provider, model, data, mime, prompt, maxTokens)
	if err == nil {
		if formatter != nil && formatterModel != "" {
			visual, formatErr := h.callVisualFormatProvider(ctx, formatter, formatterModel, raw)
			if formatErr == nil {
				return visual, nil
			}
			return parseGoogleDriveVisualPayload(raw)
		}
		return parseGoogleDriveVisualPayload(raw)
	}

	raw, retryErr := h.callVisualIndexProviderRaw(ctx, provider, model, data, mime, googleDriveVisualRetryPrompt, 1200)
	if retryErr == nil {
		return parseGoogleDriveVisualPayload(raw)
	}
	return googleDriveVisualPayload{}, fmt.Errorf("%w; retry failed: %v", err, retryErr)
}

func prepareGoogleDriveVisualImage(filePath string) ([]byte, string, error) {
	mime, ok := googleDriveImageMime(filePath)
	if !ok {
		return nil, "", fmt.Errorf("unsupported image type")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("stat image: %w", err)
	}
	if info.Size() <= googleDriveVisualMaxImageBytes {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, "", fmt.Errorf("read image: %w", err)
		}
		return data, mime, nil
	}

	img, err := imaging.Open(filePath, imaging.AutoOrientation(true))
	if err != nil {
		return nil, "", fmt.Errorf("decode large image preview: %w", err)
	}
	data, err := encodeGoogleDriveVisualPreview(img)
	if err != nil {
		return nil, "", err
	}
	if len(data) > googleDriveVisualMaxImageBytes {
		return nil, "", fmt.Errorf("image preview too large: %d bytes", len(data))
	}
	return data, "image/jpeg", nil
}

func encodeGoogleDriveVisualPreview(img image.Image) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("empty image")
	}
	for _, side := range []int{googleDriveVisualPreviewMaxSide, 1280, 1024} {
		preview := imaging.Fit(img, side, side, imaging.Lanczos)
		for _, quality := range []int{85, 78, 70} {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, preview, &jpeg.Options{Quality: quality}); err != nil {
				return nil, fmt.Errorf("encode image preview: %w", err)
			}
			if buf.Len() <= googleDriveVisualMaxImageBytes {
				return buf.Bytes(), nil
			}
		}
	}
	return nil, fmt.Errorf("image preview too large after resize")
}

func (h *MCPHandler) callVisualFormatProvider(ctx context.Context, provider providers.Provider, model, caption string) (googleDriveVisualPayload, error) {
	caption = cleanGoogleDriveVisualCaption(caption)
	if caption == "" {
		return googleDriveVisualPayload{}, fmt.Errorf("empty vision caption")
	}
	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: googleDriveVisualFormatPrompt + caption,
		}},
		Model: model,
		Options: map[string]any{
			providers.OptMaxTokens:   1200,
			providers.OptTemperature: 0,
		},
	})
	if err != nil {
		return googleDriveVisualPayload{}, err
	}
	visual, err := parseGoogleDriveVisualJSONPayload(resp.Content)
	if err == nil {
		return visual, nil
	}
	resp, retryErr := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: googleDriveVisualRetryPrompt + "\n\nCaption:\n" + caption,
		}},
		Model: model,
		Options: map[string]any{
			providers.OptMaxTokens:   900,
			providers.OptTemperature: 0,
		},
	})
	if retryErr != nil {
		return googleDriveVisualPayload{}, fmt.Errorf("%w; retry failed: %v", err, retryErr)
	}
	visual, retryErr = parseGoogleDriveVisualJSONPayload(resp.Content)
	if retryErr == nil {
		return visual, nil
	}
	return googleDriveVisualPayload{}, fmt.Errorf("%w; retry failed: %v", err, retryErr)
}

func (h *MCPHandler) callVisualIndexProviderRaw(ctx context.Context, provider providers.Provider, model string, data []byte, mime, prompt string, maxTokens int) (string, error) {
	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: prompt,
			Images: []providers.ImageContent{{
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(data),
			}},
		}},
		Model: model,
		Options: map[string]any{
			providers.OptMaxTokens:   maxTokens,
			providers.OptTemperature: 0,
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func parseGoogleDriveVisualPayload(raw string) (googleDriveVisualPayload, error) {
	if payload, err := parseGoogleDriveVisualJSONPayload(raw); err == nil {
		return payload, nil
	}
	if payload, ok := fallbackGoogleDriveVisualPayload(raw); ok {
		return payload, nil
	}
	return googleDriveVisualPayload{}, fmt.Errorf("vision response is empty")
}

func parseGoogleDriveVisualJSONPayload(raw string) (googleDriveVisualPayload, error) {
	candidates := googleDriveVisualJSONCandidates(raw)
	var lastErr error
	for _, candidate := range candidates {
		payload, err := decodeGoogleDriveVisualPayload(candidate)
		if err == nil {
			payload.SceneType = normalizeEnum(payload.SceneType, []string{"product", "factory", "construction", "food", "document", "people", "other"}, "other")
			payload.Quality = normalizeEnum(payload.Quality, []string{"low", "medium", "high"}, "medium")
			return payload, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return googleDriveVisualPayload{}, fmt.Errorf("vision response is not valid JSON: %w", lastErr)
	}
	return googleDriveVisualPayload{}, fmt.Errorf("vision response is not valid JSON")
}

func fallbackGoogleDriveVisualPayload(raw string) (googleDriveVisualPayload, bool) {
	caption := cleanGoogleDriveVisualCaption(raw)
	if caption == "" {
		return googleDriveVisualPayload{}, false
	}
	return googleDriveVisualPayload{
		SummaryVI:         caption,
		DescriptionVI:     caption,
		TagsVI:            []string{},
		TagsEN:            []string{},
		MainSubject:       "",
		SceneType:         "other",
		DetectedText:      []string{},
		UsableAsReference: true,
		Quality:           "medium",
	}, true
}

func cleanGoogleDriveVisualCaption(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	var wrapped struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Content  string `json:"content"`
		Response string `json:"response"`
	}
	if err := json.Unmarshal([]byte(text), &wrapped); err == nil {
		for _, candidate := range []string{wrapped.Message.Content, wrapped.Content, wrapped.Response} {
			if cleaned := cleanGoogleDriveVisualCaption(candidate); cleaned != "" {
				return cleaned
			}
		}
	}

	text = stripMarkdownFence(text)
	text = strings.TrimSpace(strings.TrimPrefix(text, "```"))
	text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "json") {
			continue
		}
		kept = append(kept, line)
	}
	text = strings.Join(kept, " ")
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 1200 {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:1200]))
	}
	return text
}

func googleDriveVisualJSONCandidates(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}

	add(raw)

	var wrapped struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Content  string `json:"content"`
		Response string `json:"response"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &wrapped); err == nil {
		add(wrapped.Message.Content)
		add(wrapped.Content)
		add(wrapped.Response)
	}

	for _, candidate := range append([]string(nil), out...) {
		add(stripMarkdownFence(candidate))
		if fenced := extractMarkdownFence(candidate); fenced != "" {
			add(fenced)
		}
		if object := extractJSONObject(candidate); object != "" {
			add(object)
		}
		if object := extractJSONObject(stripMarkdownFence(candidate)); object != "" {
			add(object)
		}
	}
	return out
}

func decodeGoogleDriveVisualPayload(candidate string) (googleDriveVisualPayload, error) {
	var payload googleDriveVisualPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &payload); err != nil {
		return payload, err
	}
	if !hasGoogleDriveVisualPayloadContent(payload) {
		return payload, fmt.Errorf("missing visual payload fields")
	}
	return payload, nil
}

func hasGoogleDriveVisualPayloadContent(payload googleDriveVisualPayload) bool {
	return strings.TrimSpace(payload.SummaryVI) != "" ||
		strings.TrimSpace(payload.DescriptionVI) != "" ||
		strings.TrimSpace(payload.MainSubject) != "" ||
		len(payload.TagsVI) > 0 ||
		len(payload.TagsEN) > 0 ||
		len(payload.DetectedText) > 0
}

func stripMarkdownFence(value string) string {
	text := strings.TrimSpace(value)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	lines = lines[1:]
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractMarkdownFence(value string) string {
	text := strings.TrimSpace(value)
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	rest := text[start+3:]
	if newline := strings.Index(rest, "\n"); newline >= 0 {
		rest = rest[newline+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func extractJSONObject(value string) string {
	text := strings.TrimSpace(value)
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1])
			}
		}
	}
	return ""
}

func applyGoogleDriveVisual(file *googleDriveFile, visual googleDriveVisualPayload) {
	file.VisualSummaryVI = strings.TrimSpace(visual.SummaryVI)
	file.VisualDescriptionVI = strings.TrimSpace(visual.DescriptionVI)
	file.VisualTagsVI = cleanStringList(visual.TagsVI, 20)
	file.VisualTagsEN = cleanStringList(visual.TagsEN, 20)
	file.VisualMainSubject = strings.TrimSpace(visual.MainSubject)
	file.VisualSceneType = visual.SceneType
	file.VisualDetectedText = cleanStringList(visual.DetectedText, 30)
	file.VisualUsableAsReference = &visual.UsableAsReference
	file.VisualQuality = visual.Quality
}

func googleDriveImageMime(filePath string) (string, bool) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".jpg", ".jpeg", ".jfif":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".webp":
		return "image/webp", true
	case ".gif":
		return "image/gif", true
	case ".bmp":
		return "image/bmp", true
	default:
		return "", false
	}
}

func writeGoogleDriveIndex(indexPath string, index googleDriveFolderIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Google Drive index: %w", err)
	}
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write Google Drive index: %w", err)
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace Google Drive index: %w", err)
	}
	return nil
}

func googleDriveItemUnderFolder(parents []string, folderID string, recursive bool, folders map[string]googleDriveFolder) bool {
	for _, parent := range parents {
		if parent == folderID {
			return true
		}
	}
	if !recursive {
		return false
	}
	for _, parent := range parents {
		if googleDriveFolderHasAncestor(parent, folderID, folders) {
			return true
		}
	}
	return false
}

func googleDriveFolderHasAncestor(folderID, ancestorID string, folders map[string]googleDriveFolder) bool {
	seen := map[string]bool{}
	for current := folderID; current != "" && !seen[current]; {
		if current == ancestorID {
			return true
		}
		seen[current] = true
		folder := folders[current]
		if len(folder.Parents) == 0 {
			return false
		}
		current = folder.Parents[0]
	}
	return false
}

func parseGoogleDriveID(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}
	for _, marker := range []string{"/folders/", "/file/d/"} {
		if idx := strings.Index(raw, marker); idx >= 0 {
			rest := raw[idx+len(marker):]
			if cut := strings.IndexAny(rest, "/?&#"); cut >= 0 {
				rest = rest[:cut]
			}
			return strings.TrimSpace(rest)
		}
	}
	if idx := strings.Index(raw, "id="); idx >= 0 {
		rest := raw[idx+3:]
		if cut := strings.IndexAny(rest, "&#"); cut >= 0 {
			rest = rest[:cut]
		}
		return strings.TrimSpace(rest)
	}
	return raw
}

func normalizeEnum(value string, allowed []string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if value == item {
			return item
		}
	}
	return fallback
}

func cleanStringList(values []string, limit int) []string {
	out := make([]string, 0, min(len(values), limit))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func tailStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[len(values)-limit:]
}

func safeDriveCacheSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "item"
	}
	return out
}
