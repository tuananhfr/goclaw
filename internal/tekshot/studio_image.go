package tekshot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const studioReferenceMaxBytes = 10 << 20

// StudioImageDeps is what studio_image needs outside the agent loop.
type StudioImageDeps struct {
	Providers    *providers.Registry
	BuiltinTools store.BuiltinToolStore
	Skills       store.SkillStore
	Workspace    string // GoClaw workspace root; /v1/files serves it to Drupal
}

// SetStudioImageDeps wires studio_image. A setter keeps NewJobService's
// signature (tests build &JobService{} directly).
func (s *JobService) SetStudioImageDeps(deps StudioImageDeps) {
	s.studio = &deps
}

func (s *JobService) runStudioImage(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.studio == nil || s.studio.Providers == nil || s.studio.BuiltinTools == nil || s.studio.Workspace == "" {
		return nil, "", fmt.Errorf("studio_image is not wired (provider registry, builtin tool store or workspace missing)")
	}
	req, err := parseStudioImageRequest(request)
	if err != nil {
		return nil, "", err
	}
	ctx = store.WithTenantID(ctx, store.MasterTenantID)
	settings, err := s.studio.BuiltinTools.GetSettings(ctx, "create_image")
	if err != nil {
		return nil, "", fmt.Errorf("studio_image: load create_image settings: %w", err)
	}
	ctx = tools.WithBuiltinToolSettings(ctx, tools.BuiltinToolSettings{"create_image": settings})

	images, err := loadStudioImages(req.Media, []string{os.TempDir(), s.studio.Workspace})
	if err != nil {
		return nil, "", err
	}
	prompt := appendSkillsBlock(req.Prompt, s.loadTaggedSkills(ctx, job, req.TaggedSkills))

	var chosen referenceLibraryItem
	if len(req.Library) > 0 {
		chosen = s.chooseStudioReference(ctx, job, req.Prompt, req.Library)
		if chosen.ID > 0 {
			img, dlErr := s.downloadStudioReference(ctx, chosen.URL)
			if dlErr != nil {
				slog.Warn("tekshot: studio reference download failed, drawing without it",
					"job", job.ID.String(), "reference_image_id", chosen.ID, "error", dlErr)
				chosen = referenceLibraryItem{}
			} else {
				images = append(capStudioImages(images, studioImageMaxImages-1), img)
				prompt = appendLibraryLine(prompt, chosen)
			}
		}
	}
	images = capStudioImages(images, studioImageMaxImages)

	s.setProgress(ctx, job, "Đang vẽ ảnh")
	var result *providers.StudioImageResult
	// The chain retries every entry on any error and reports the last one, so a
	// non-Codex fallback would bury the real failure and a second Codex entry
	// would redraw a refusal (~50s). Keep the Codex error; stop on a final one.
	var drawErr error
	drawFinal := false
	err = tools.RunCreateImageChain(ctx, s.studio.Providers, func(ctx context.Context, target tools.ImageChainTarget) error {
		if drawFinal {
			return drawErr
		}
		sp, ok := target.Provider.(providers.StudioImageProvider)
		if !ok {
			return fmt.Errorf("provider %s cannot run studio_image (needs Codex / ChatGPT OAuth)", target.Provider.Name())
		}
		res, callErr := sp.StudioImage(ctx, providers.StudioImageRequest{
			Model:        target.Model,
			ImageModel:   target.ImageModel,
			Quality:      target.Quality,
			Size:         req.Size,
			Instructions: req.Instructions,
			Text:         prompt,
			Images:       images,
		})
		if callErr != nil {
			drawErr = callErr
			drawFinal = !providers.IsRetryableError(callErr)
			return callErr
		}
		result = res
		return nil
	})
	if err != nil {
		if drawErr != nil {
			err = drawErr
		}
		slog.Warn("tekshot: studio image failed", "job", job.ID.String(), "error", err)
		return nil, "", studioImageUserError(err)
	}

	path, err := writeStudioImage(s.studio.Workspace, job, result.Data)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{
		"content": result.Text,
		"media": []agent.MediaResult{{
			Path:        path,
			ContentType: "image/png",
			Size:        int64(len(result.Data)),
			Prompt:      result.RevisedPrompt,
		}},
		"action":             result.Action,
		"reference_image_id": chosen.ID,
		"usage":              result.Usage,
	}, "Completed", nil
}

func (s *JobService) loadTaggedSkills(ctx context.Context, job *store.TekshotJob, names []string) []loadedSkill {
	if s.studio.Skills == nil {
		return nil
	}
	skills := make([]loadedSkill, 0, len(names))
	for _, name := range names {
		content, ok := s.studio.Skills.LoadSkill(ctx, name)
		if !ok || strings.TrimSpace(content) == "" {
			slog.Warn("tekshot: tagged skill not found, skipping", "job", job.ID.String(), "skill", name)
			continue
		}
		skills = append(skills, loadedSkill{Name: name, Content: content})
	}
	return skills
}

// chooseStudioReference is reference_choice.go's pick, called on the model
// directly: same prompt, parser and one retry on an unreadable reply.
func (s *JobService) chooseStudioReference(ctx context.Context, job *store.TekshotJob, brief string, items []referenceLibraryItem) referenceLibraryItem {
	shortlist := capReferenceItems(items, referenceChoiceMaxItems)
	prompt := buildReferenceChoicePrompt(brief, shortlist)
	for attempt := 1; attempt <= 2; attempt++ {
		var reply string
		choiceCtx, cancel := context.WithTimeout(ctx, referenceChoiceTimeout)
		err := tools.RunCreateImageChain(choiceCtx, s.studio.Providers, func(ctx context.Context, target tools.ImageChainTarget) error {
			resp, callErr := target.Provider.Chat(ctx, providers.ChatRequest{
				Messages: []providers.Message{{Role: "user", Content: prompt}},
				Model:    target.Model,
				Options:  map[string]any{providers.OptThinkingLevel: "low"},
			})
			if callErr != nil {
				return callErr
			}
			reply = resp.Content
			return nil
		})
		cancel()
		if err != nil {
			slog.Warn("tekshot: studio reference choice failed, drawing without a library image",
				"job", job.ID.String(), "attempt", attempt, "error", err)
			return referenceLibraryItem{}
		}
		if _, parsed := referenceChoiceRawID(reply); !parsed {
			slog.Warn("tekshot: studio reference choice reply carried no id",
				"job", job.ID.String(), "attempt", attempt, "reply_head", headRunes(reply, 200))
			continue
		}
		id := parseReferenceChoice(reply, shortlist)
		for _, item := range shortlist {
			if item.ID == id {
				slog.Info("tekshot: studio reference image chosen", "job", job.ID.String(), "reference_image_id", id)
				return item
			}
		}
		return referenceLibraryItem{}
	}
	return referenceLibraryItem{}
}

func (s *JobService) downloadStudioReference(ctx context.Context, url string) (providers.ImageContent, error) {
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return providers.ImageContent{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return providers.ImageContent{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providers.ImageContent{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, studioReferenceMaxBytes+1))
	if err != nil {
		return providers.ImageContent{}, err
	}
	if len(data) > studioReferenceMaxBytes {
		return providers.ImageContent{}, fmt.Errorf("reference image larger than %d bytes", studioReferenceMaxBytes)
	}
	return imageContentFromBytes(data)
}

func loadStudioImages(media []studioImageMedia, roots []string) ([]providers.ImageContent, error) {
	images := make([]providers.ImageContent, 0, len(media))
	for _, item := range media {
		path, err := resolveStudioMediaPath(item.Path, roots)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("studio_image: read media: %w", err)
		}
		img, err := imageContentFromBytes(data)
		if err != nil {
			return nil, fmt.Errorf("studio_image: %s: %w", filepath.Base(path), err)
		}
		images = append(images, img)
	}
	return images, nil
}

func imageContentFromBytes(data []byte) (providers.ImageContent, error) {
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return providers.ImageContent{}, fmt.Errorf("not an image (%s)", mime)
	}
	return providers.ImageContent{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

// capStudioImages trims from the end: the base image is always first.
func capStudioImages(images []providers.ImageContent, max int) []providers.ImageContent {
	if max < 0 || len(images) <= max {
		return images
	}
	return images[:max]
}

func writeStudioImage(workspace string, job *store.TekshotJob, data []byte) (string, error) {
	dir := filepath.Join(workspace, "tekshot_studio", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("studio_image: create output dir: %w", err)
	}
	path := filepath.Join(dir, "studio-"+job.ID.String()+".png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("studio_image: write image: %w", err)
	}
	return path, nil
}

// studioImageUserError is what the image chat error box shows: Drupal stores
// the job error verbatim, so it must read as Vietnamese, not a Go error chain.
func studioImageUserError(err error) error {
	var refusal *providers.StudioImageRefusal
	if errors.As(err, &refusal) {
		if text := strings.TrimSpace(refusal.Text); text != "" {
			return errors.New(text)
		}
		return errors.New("Máy vẽ không trả về ảnh. Thử diễn đạt lại yêu cầu.")
	}
	if providers.IsRetryableError(err) {
		return errors.New("Máy vẽ đang quá tải, thử lại sau ít phút.")
	}
	return fmt.Errorf("studio_image: %w", err)
}
