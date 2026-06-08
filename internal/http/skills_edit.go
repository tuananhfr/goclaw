package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const maxSkillEditContentSize = 100 * 1024

type updateSkillMDRequest struct {
	Content string `json:"content"`
}

func (h *SkillsHandler) handleUpdateSkillMD(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	var req updateSkillMDRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSkillEditContentSize+1024)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "invalid JSON body")})
		return
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "SKILL.md is empty")})
		return
	}
	if len(content) > maxSkillEditContentSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, fmt.Sprintf("SKILL.md too large (%d bytes, max %d)", len(content), maxSkillEditContentSize))})
		return
	}

	info, ok := h.skills.GetSkillByID(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
		return
	}
	if info.IsSystem {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "system skill edit")})
		return
	}
	if info.Status != "" && info.Status != "active" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "only active skills can be edited")})
		return
	}
	if info.BaseDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "skill file path is not available")})
		return
	}

	violations, safe := skills.GuardSkillContent(content)
	if !safe {
		slog.Warn("security.skills.edit_rejected",
			"user_id", userID,
			"skill_id", id,
			"violations", len(violations),
			"first_rule", violations[0].Reason)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":      i18n.T(locale, i18n.MsgInvalidRequest, "skill content failed security scan"),
			"violations": skills.FormatGuardViolations(violations),
		})
		return
	}

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(content)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name in SKILL.md frontmatter")})
		return
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if slug != info.Slug {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "SKILL.md slug/name cannot change in editor; upload as a new skill instead")})
		return
	}
	if !skills.SlugRegexp.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	contentBytes := []byte(content)
	skillHash := fmt.Sprintf("%x", sha256.Sum256(contentBytes))

	tenantSkillsBase := h.tenantSkillsDir(r)
	editLock := h.skillUploadLock(filepath.Join(tenantSkillsBase, slug))
	editLock.Lock()
	defer editLock.Unlock()

	version := h.skills.GetNextVersion(r.Context(), slug)
	destDir := filepath.Join(tenantSkillsBase, slug, fmt.Sprintf("%d", version))
	if err := copySkillVersionDir(info.BaseDir, destDir); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to copy current skill version")})
		return
	}
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), contentBytes, 0644); err != nil {
		_ = os.RemoveAll(destDir)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to write SKILL.md")})
		return
	}

	desc := description
	skill := store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Folder:      normalizeSkillFolder(frontmatter["folder"]),
		Description: &desc,
		OwnerID:     userID,
		Visibility:  info.Visibility,
		Version:     version,
		FilePath:    destDir,
		FileSize:    int64(len(contentBytes)),
		FileHash:    &skillHash,
		Frontmatter: frontmatter,
		Status:      "active",
	}

	response := map[string]any{"id": id, "slug": slug, "version": version, "name": name, "status": "active"}
	depState := uploadSkillDepState{}
	depsCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), uploadDepsInstallTimeout)
	defer cancel()
	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		if ok, missing := checkUploadedSkillDeps(manifest); !ok {
			depState = h.reconcileUploadedSkillDeps(
				depsCtx,
				slug,
				manifest,
				missing,
				canAutoInstallUploadedSkillDeps(r.Context()),
			)
			skill.Status = depState.status
			skill.MissingDeps = depState.missing
			for k, v := range depState.response {
				response[k] = v
			}
		}
	}

	returnedID, err := h.skills.CreateSkillManaged(depsCtx, skill)
	if err != nil {
		_ = os.RemoveAll(destDir)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "skill", err.Error())})
		return
	}
	response["id"] = returnedID

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkills, returnedID.String(), uuid.Nil)
	emitAudit(h.msgBus, r, "skill.updated", "skill", slug)
	slog.Info("skill updated from editor", "id", returnedID, "slug", slug, "old_version", info.Version, "version", version, "status", skill.Status)
	depState.emit(h, slug)

	writeJSON(w, http.StatusOK, response)
}

func copySkillVersionDir(srcDir, dstDir string) error {
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	var totalSize int64
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.Contains(rel, "..") || skills.IsSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dstPath := filepath.Join(dstDir, rel)
		if !strings.HasPrefix(dstPath, dstDir+string(filepath.Separator)) {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += fi.Size()
		if totalSize > maxSkillUploadSize {
			return fmt.Errorf("skill version exceeds %d bytes limit", maxSkillUploadSize)
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, src)
		return err
	})
}
