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
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

const studioReferenceMaxBytes = 10 << 20

// StudioImageDeps is what studio_image needs outside the agent loop.
type StudioImageDeps struct {
	Providers      *providers.Registry
	BuiltinTools   store.BuiltinToolStore
	Skills         store.SkillStore
	Workspace      string // GoClaw workspace root; /v1/files serves it to Drupal
	TraceCollector *tracing.Collector
	Agents         store.AgentCRUDStore
}

// SetStudioImageDeps wires studio_image. A setter keeps NewJobService's
// signature (tests build &JobService{} directly).
func (s *JobService) SetStudioImageDeps(deps StudioImageDeps) {
	s.studio = &deps
}

func (s *JobService) runStudioImage(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	ctx = s.withStudioImageAgent(ctx, job)
	_, finishPrepare := startStudioImageSpan(ctx, store.SpanData{Name: "Prepare image request", SpanType: store.SpanTypeEvent},
		map[string]any{"prompt": stringFromMap(request, "prompt"), "instructions": stringFromMap(request, "instructions")})
	if s.studio == nil || s.studio.Providers == nil || s.studio.BuiltinTools == nil || s.studio.Workspace == "" {
		err := fmt.Errorf("studio_image is not wired (provider registry, builtin tool store or workspace missing)")
		finishPrepare(nil, err, nil)
		return nil, "", err
	}
	req, err := parseStudioImageRequest(request)
	if err != nil {
		finishPrepare(nil, err, nil)
		return nil, "", err
	}
	workspace, err := s.studioImageWorkspace(ctx)
	if err != nil {
		finishPrepare(nil, err, nil)
		return nil, "", err
	}
	ctx = store.WithTenantID(ctx, store.MasterTenantID)
	settings, err := s.studio.BuiltinTools.GetSettings(ctx, "create_image")
	if err != nil {
		finishPrepare(nil, err, nil)
		return nil, "", fmt.Errorf("studio_image: load create_image settings: %w", err)
	}
	ctx = tools.WithBuiltinToolSettings(ctx, tools.BuiltinToolSettings{"create_image": settings})

	images, err := loadStudioImages(req.Media, []string{os.TempDir(), s.studio.Workspace})
	if err != nil {
		finishPrepare(nil, err, nil)
		return nil, "", err
	}
	prompt := appendSkillsBlock(req.Prompt, s.loadTaggedSkills(ctx, job, req.TaggedSkills))
	finishPrepare(map[string]any{"image_count": len(images), "tagged_skills": req.TaggedSkills, "size": req.Size, "workspace": workspace}, nil, nil)

	var chosen referenceLibraryItem
	if len(req.Library) > 0 {
		chosen = s.chooseStudioReference(ctx, job, req.Prompt, req.Library)
		if chosen.ID > 0 {
			_, finishDownload := startStudioImageSpan(ctx, store.SpanData{Name: "Download reference image", SpanType: store.SpanTypeEvent},
				map[string]any{"reference_image_id": chosen.ID})
			img, dlErr := s.downloadStudioReference(ctx, chosen.URL)
			finishDownload(map[string]any{"mime_type": img.MimeType, "encoded_bytes": len(img.Data)}, dlErr, nil)
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
	drawCtx, finishDraw := startStudioImageSpan(ctx, store.SpanData{Name: "Generate image", SpanType: store.SpanTypeEvent},
		map[string]any{"instructions": req.Instructions, "prompt": prompt, "size": req.Size, "image_count": len(images), "reference_image_id": chosen.ID})
	err = tools.RunCreateImageChain(drawCtx, s.studio.Providers, func(ctx context.Context, target tools.ImageChainTarget) error {
		if drawFinal {
			return drawErr
		}
		sp, ok := target.Provider.(providers.StudioImageProvider)
		if !ok {
			return fmt.Errorf("provider %s cannot run studio_image (needs Codex / ChatGPT OAuth)", target.Provider.Name())
		}
		observation := providers.NewChatGPTOAuthRoutingObservation()
		callCtx := providers.WithChatGPTOAuthRoutingObservation(ctx, observation)
		callCtx, finishAttempt := startStudioImageSpan(callCtx, store.SpanData{Name: "Try image provider", SpanType: store.SpanTypeEvent, Provider: target.Provider.Name(), Model: target.Model}, nil)
		res, callErr := sp.StudioImage(callCtx, providers.StudioImageRequest{
			Model:        target.Model,
			ImageModel:   target.ImageModel,
			Quality:      target.Quality,
			Size:         req.Size,
			Instructions: req.Instructions,
			Text:         prompt,
			Images:       images,
		})
		finishAttempt(studioImageResultSummary(res), callErr, nil)
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
		finishDraw(nil, err, nil)
		if imageTrace := studioImageTraceFromContext(ctx); imageTrace != nil {
			imageTrace.rawError = err
		}
		slog.Warn("tekshot: studio image failed", "job", job.ID.String(), "error", err)
		return nil, "", studioImageUserError(err)
	}
	finishDraw(studioImageResultSummary(result), nil, nil)

	_, finishSave := startStudioImageSpan(ctx, store.SpanData{Name: "Save generated image", SpanType: store.SpanTypeEvent}, nil)
	path, err := writeStudioImage(workspace, job, result.Data)
	finishSave(map[string]any{"path": path, "image_bytes": len(result.Data)}, err, nil)
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
			_, finish := startStudioImageSpan(ctx, store.SpanData{Name: "Load tagged image skill", SpanType: store.SpanTypeEvent}, map[string]any{"skill": name})
			finish("Continuing without this skill", fmt.Errorf("tagged skill %q is missing or empty", name), nil)
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
			observation := providers.NewChatGPTOAuthRoutingObservation()
			callCtx := providers.WithChatGPTOAuthRoutingObservation(ctx, observation)
			callCtx, finish := startStudioImageSpan(callCtx, store.SpanData{
				Name: "Choose reference image", SpanType: store.SpanTypeLLMCall, Provider: target.Provider.Name(), Model: target.Model,
			}, map[string]any{"prompt": prompt, "parse_attempt": attempt})
			if imageTrace := studioImageTraceFromContext(callCtx); imageTrace != nil {
				callCtx = providers.WithRetryHook(callCtx, func(attempt, maxAttempts int, err error) {
					imageTrace.recordRetry(callCtx, target.Provider.Name(), attempt, maxAttempts, err)
				})
			}
			resp, callErr := target.Provider.Chat(callCtx, providers.ChatRequest{
				Messages: []providers.Message{{Role: "user", Content: prompt}},
				Model:    target.Model,
				Options:  map[string]any{providers.OptThinkingLevel: "low"},
			})
			if callErr != nil {
				finish(nil, callErr, nil)
				return callErr
			}
			if resp == nil {
				err := fmt.Errorf("reference selection returned no response")
				finish(nil, err, nil)
				return err
			}
			finish(resp.Content, nil, resp.Usage)
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
			_, finish := startStudioImageSpan(ctx, store.SpanData{Name: "Parse reference selection", SpanType: store.SpanTypeEvent}, reply)
			finish(map[string]any{"parse_attempt": attempt, "will_retry": attempt < 2}, fmt.Errorf("reference selection reply contained no readable id"), nil)
			slog.Warn("tekshot: studio reference choice reply carried no id",
				"job", job.ID.String(), "attempt", attempt, "reply_head", headRunes(reply, 200))
			continue
		}
		id := parseReferenceChoice(reply, shortlist)
		for _, item := range shortlist {
			if item.ID == id {
				_, finish := startStudioImageSpan(ctx, store.SpanData{Name: "Select reference image", SpanType: store.SpanTypeEvent}, nil)
				finish(map[string]any{"reference_image_id": id}, nil, nil)
				slog.Info("tekshot: studio reference image chosen", "job", job.ID.String(), "reference_image_id", id)
				return item
			}
		}
		_, finish := startStudioImageSpan(ctx, store.SpanData{Name: "Select reference image", SpanType: store.SpanTypeEvent}, nil)
		finish("Drawing without a library image", nil, nil)
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
