package http

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const vaultContentMaxBytes = 1_048_576

type vaultDocumentContentResponse struct {
	Document *store.VaultDocument `json:"document"`
	Content  string               `json:"content"`
}

type vaultDocumentContentUpdateResponse struct {
	Document         *store.VaultDocument `json:"document"`
	EnrichmentQueued bool                 `json:"enrichment_queued"`
}

func (h *VaultHandler) handleGetDocumentContent(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	doc, ok := h.loadAuthorizedVaultContentDocument(w, r, tenantID.String())
	if !ok {
		return
	}

	wsPath := h.resolveTenantWorkspace(r.Context())
	if wsPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
		return
	}
	fullPath, err := resolveVaultDocumentFilePath(wsPath, doc.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "document file not found"})
			return
		}
		slog.Warn("vault.content.get stat failed", "doc_id", doc.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to stat document file"})
		return
	}
	if info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "document path is a directory"})
		return
	}
	if info.Size() > vaultContentMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "document content exceeds 1 MB limit"})
		return
	}

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		slog.Warn("vault.content.get read failed", "doc_id", doc.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read document file"})
		return
	}
	if !utf8.Valid(contentBytes) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "document content is not valid UTF-8 text"})
		return
	}

	writeJSON(w, http.StatusOK, vaultDocumentContentResponse{
		Document: doc,
		Content:  string(contentBytes),
	})
}

func (h *VaultHandler) handleUpdateDocumentContent(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())
	tenantIDStr := tenantID.String()
	doc, ok := h.loadAuthorizedVaultContentDocument(w, r, tenantIDStr)
	if !ok {
		return
	}

	var body struct {
		Title    *string        `json:"title"`
		Content  *string        `json:"content"`
		DocType  *string        `json:"doc_type"`
		Metadata map[string]any `json:"metadata"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Content == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	if strings.TrimSpace(*body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content cannot be empty"})
		return
	}
	if len([]byte(*body.Content)) > vaultContentMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "document content exceeds 1 MB limit"})
		return
	}
	if !utf8.ValidString(*body.Content) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content must be valid UTF-8 text"})
		return
	}

	title := doc.Title
	if body.Title != nil {
		title = strings.TrimSpace(*body.Title)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title cannot be empty"})
			return
		}
	}
	docType := doc.DocType
	if body.DocType != nil {
		docType = strings.TrimSpace(*body.DocType)
		if !validDocType(docType) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid doc_type"})
			return
		}
		if docType == "media" || docType == "document" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "document content endpoint supports text documents only"})
			return
		}
	}
	metadata := doc.Metadata
	if body.Metadata != nil {
		metadata = body.Metadata
	}

	wsPath := h.resolveTenantWorkspace(r.Context())
	if wsPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
		return
	}
	fullPath, err := resolveVaultDocumentFilePath(wsPath, doc.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	hash, err := writeVaultDocumentContentAtomic(fullPath, *body.Content)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "document file not found"})
			return
		}
		slog.Warn("vault.content.update write failed", "doc_id", doc.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write document content"})
		return
	}

	updated, err := h.store.UpdateDocumentAfterContentWrite(r.Context(), tenantIDStr, doc.ID, title, docType, metadata, hash)
	if err != nil {
		slog.Warn("vault.content.update store failed", "doc_id", doc.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}

	enrichmentQueued := false
	if h.eventBus != nil {
		agentForEvent := ""
		if updated.AgentID != nil {
			agentForEvent = *updated.AgentID
		}
		if h.enrichProgress != nil {
			h.enrichProgress.Start(1, tenantID)
		}
		h.eventBus.Publish(eventbus.DomainEvent{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Type:      eventbus.EventVaultDocUpserted,
			SourceID:  updated.ID + ":" + hash,
			TenantID:  tenantIDStr,
			AgentID:   agentForEvent,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       updated.ID,
				TenantID:    tenantIDStr,
				AgentID:     agentForEvent,
				Path:        updated.Path,
				ContentHash: hash,
				Workspace:   wsPath,
			},
		})
		enrichmentQueued = true
	}

	writeJSON(w, http.StatusOK, vaultDocumentContentUpdateResponse{
		Document:         updated,
		EnrichmentQueued: enrichmentQueued,
	})
}

func (h *VaultHandler) loadAuthorizedVaultContentDocument(w http.ResponseWriter, r *http.Request, tenantID string) (*store.VaultDocument, bool) {
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	doc, err := h.store.GetDocumentByID(r.Context(), tenantID, docID)
	if err != nil || doc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return nil, false
	}
	if agentID != "" && (doc.AgentID == nil || *doc.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return nil, false
	}
	if doc.TeamID != nil && *doc.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *doc.TeamID) {
			return nil, false
		}
	}
	if err := ensureVaultDocumentTextEditable(doc); err != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": err.Error()})
		return nil, false
	}
	return doc, true
}

func ensureVaultDocumentTextEditable(doc *store.VaultDocument) error {
	if doc == nil {
		return fmt.Errorf("document not found")
	}
	if doc.DocType == "media" || doc.DocType == "document" {
		return fmt.Errorf("document content endpoint supports text documents only")
	}
	ext := strings.ToLower(filepath.Ext(doc.Path))
	if ext == "" || !allowedUploadExts[ext] {
		return fmt.Errorf("unsupported file type for text content edit")
	}
	return nil
}

func resolveVaultDocumentFilePath(workspaceRoot, relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("document path is empty")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("document path must be relative")
	}

	rel := filepath.Clean(filepath.FromSlash(relPath))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("document path outside workspace")
	}

	wsClean := filepath.Clean(workspaceRoot)
	wsResolved := wsClean
	if resolved, err := filepath.EvalSymlinks(wsClean); err == nil {
		wsResolved = resolved
	}

	fullPath := filepath.Join(wsResolved, rel)
	resolvedPath := fullPath
	if resolved, err := filepath.EvalSymlinks(fullPath); err == nil {
		resolvedPath = resolved
	}
	if !vaultHTTPPathUnder(resolvedPath, wsResolved) {
		return "", fmt.Errorf("document path outside workspace")
	}

	parent := filepath.Dir(fullPath)
	parentResolved := parent
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parentResolved = resolved
	}
	if !vaultHTTPPathUnder(parentResolved, wsResolved) {
		return "", fmt.Errorf("document path outside workspace")
	}
	if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("document path cannot be a symlink")
	}

	return fullPath, nil
}

func vaultHTTPPathUnder(child, parent string) bool {
	childClean := filepath.Clean(child)
	parentClean := filepath.Clean(parent)
	if childClean == parentClean {
		return true
	}
	return strings.HasPrefix(childClean, parentClean+string(os.PathSeparator))
}

func writeVaultDocumentContentAtomic(path, content string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("document path is a directory")
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	if _, err := hasher.Write([]byte(content)); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return "", err
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return "", err
		}
		if renameErr := os.Rename(tmpPath, path); renameErr != nil {
			return "", renameErr
		}
	}
	removeTmp = false
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
