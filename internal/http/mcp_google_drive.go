package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type googleDriveMCPSettings struct {
	GoogleDrive *struct {
		RootFolderID   string `json:"root_folder_id"`
		RootFolderName string `json:"root_folder_name"`
		CacheDir       string `json:"cache_dir"`
	} `json:"google_drive"`
}

type googleDriveFolderIndex struct {
	RootFolderID string                       `json:"root_folder_id"`
	Files        map[string]googleDriveFile   `json:"files"`
	Folders      map[string]googleDriveFolder `json:"folders"`
	PublicImports map[string]googleDriveFile  `json:"public_imports"`
	Status       googleDriveSyncStatus        `json:"status"`
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
	Trashed      bool     `json:"trashed,omitempty"`
}

type googleDriveFile struct {
	DriveFileID  string   `json:"drive_file_id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mime_type"`
	Parents      []string `json:"parents"`
	ModifiedTime string   `json:"modified_time,omitempty"`
	Size         int64    `json:"size,omitempty"`
	LocalPath    string   `json:"local_path,omitempty"`
	Media        string   `json:"media,omitempty"`
	Trashed      bool     `json:"trashed,omitempty"`
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
		"folders": folders,
		"status":  "ok",
		"sync":    index.Status,
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

	tools, err := mcpbridge.DiscoverTools(r.Context(), srv.Transport, srv.Command, args, env, srv.URL, headers)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if h.poolEvictor != nil {
		tid := store.TenantIDFromContext(r.Context())
		h.poolEvictor.Evict(tid, srv.Name)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "sync_started",
		"tool_count": len(tools),
	})
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
