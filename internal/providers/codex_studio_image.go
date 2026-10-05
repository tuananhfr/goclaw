package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StudioImageRequest is one stateless Tekshot Studio image turn: the model
// reads the whole brief (post + request) itself, unlike NativeImageRequest
// whose prompt was already rewritten by an agent.
type StudioImageRequest struct {
	Model           string // parent LLM; empty = provider default
	ImageModel      string
	Quality         string
	Size            string // "1024x1024" | ... | "auto"
	Instructions    string
	Text            string
	ReasoningEffort string
	Images          []ImageContent // base image first
}

// StudioImageRefusal: the model answered in text instead of drawing. Typed so
// callers can stop the provider chain and show the model's own words.
type StudioImageRefusal struct {
	Text string
}

func (e *StudioImageRefusal) Error() string {
	return fmt.Sprintf("codex studio image: no image returned; model said: %q", e.Text)
}

type StudioImageResult struct {
	MimeType      string
	Data          []byte
	Text          string
	RevisedPrompt string
	Action        string
	Usage         *Usage
}

type StudioImageProvider interface {
	StudioImage(ctx context.Context, req StudioImageRequest) (*StudioImageResult, error)
}

var (
	_ StudioImageProvider = (*CodexProvider)(nil)
	_ StudioImageProvider = (*CodexAdapter)(nil)
)

func (p *CodexProvider) StudioImage(ctx context.Context, req StudioImageRequest) (result *StudioImageResult, err error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("codex studio image: empty message")
	}
	imageModel, err := ValidateImageModel(req.ImageModel)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	req.Model = model
	req.ImageModel = imageModel
	ctx, finish := observeStudioImage(ctx, p.Name(), req)
	defer func() { finish(result, err) }()
	ctx = withStudioImageRetryObservation(ctx, p.Name())
	body := buildStudioImageRequestBody(model, imageModel, req)
	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, fmt.Errorf("codex studio image: %w", err)
	}
	defer respBody.Close()
	raw, err := io.ReadAll(respBody)
	if err != nil {
		return nil, fmt.Errorf("codex studio image: read response: %w", err)
	}
	return parseStudioImageSSE(raw)
}

func (a *CodexAdapter) StudioImage(ctx context.Context, req StudioImageRequest) (*StudioImageResult, error) {
	p := &CodexProvider{
		name:         "codex",
		apiBase:      a.apiBase,
		defaultModel: a.defaultModel,
		client:       NewDefaultHTTPClient(),
		retryConfig:  DefaultRetryConfig(),
		tokenSource:  a.tokenSource,
	}
	return p.StudioImage(ctx, req)
}

func buildStudioImageRequestBody(model, imageModel string, req StudioImageRequest) map[string]any {
	content := make([]map[string]any, 0, len(req.Images)+1)
	for _, img := range req.Images {
		if img.Data == "" {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": fmt.Sprintf("data:%s;base64,%s", mime, img.Data),
		})
	}
	content = append(content, map[string]any{"type": "input_text", "text": req.Text})

	size := req.Size
	if size == "" {
		size = "auto"
	}
	tool := map[string]any{
		"type":          "image_generation",
		"action":        ImageActionAuto,
		"model":         imageModel,
		"output_format": "png",
		"size":          size,
	}
	if req.Quality != "" {
		tool["quality"] = req.Quality
	}
	effort := req.ReasoningEffort
	if effort == "" {
		effort = "low"
	}
	return map[string]any{
		"model":        model,
		"instructions": req.Instructions,
		"stream":       true,
		"store":        false,
		"input":        []any{map[string]any{"role": "user", "content": content}},
		"tools":        []map[string]any{tool},
		"tool_choice":  map[string]any{"type": "image_generation"},
		"reasoning":    map[string]any{"effort": effort},
	}
}

type studioSSEItem struct {
	Type          string `json:"type"`
	Result        string `json:"result"`
	OutputFormat  string `json:"output_format"`
	RevisedPrompt string `json:"revised_prompt"`
	Action        string `json:"action"`
}

type studioSSEEvent struct {
	Type     string         `json:"type"`
	Delta    string         `json:"delta"`
	Item     *studioSSEItem `json:"item"`
	Response *struct {
		Output []studioSSEItem `json:"output"`
		Usage  *codexUsage     `json:"usage"`
	} `json:"response"`
}

// parseStudioImageSSE keeps the model's short reply and revised prompt, which
// parseNativeImageSSE drops, and surfaces refusals instead of "no image".
func parseStudioImageSSE(data []byte) (*StudioImageResult, error) {
	res := &StudioImageResult{}
	var text strings.Builder
	var b64, format, failure string

	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := line[len("data: "):]
		if bytes.Equal(payload, []byte("[DONE]")) {
			break
		}
		var ev studioSSEEvent
		if json.Unmarshal(payload, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			text.WriteString(ev.Delta)
		case "response.output_item.done":
			if ev.Item != nil && ev.Item.Type == "image_generation_call" && ev.Item.Result != "" {
				b64, format = ev.Item.Result, ev.Item.OutputFormat
				res.RevisedPrompt, res.Action = ev.Item.RevisedPrompt, ev.Item.Action
			}
		case "response.completed", "response.incomplete":
			if ev.Response == nil {
				continue
			}
			for _, item := range ev.Response.Output {
				if item.Type == "image_generation_call" && item.Result != "" && b64 == "" {
					b64, format = item.Result, item.OutputFormat
					res.RevisedPrompt, res.Action = item.RevisedPrompt, item.Action
				}
			}
			if u := ev.Response.Usage; u != nil {
				res.Usage = &Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens}
			}
		case "response.failed", "error":
			failure = string(payload)
		}
	}

	res.Text = strings.TrimSpace(text.String())
	if b64 == "" {
		if failure != "" {
			return nil, fmt.Errorf("codex studio image: stream failed: %s", headString(failure, 500))
		}
		return nil, &StudioImageRefusal{Text: res.Text}
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("codex studio image: decode image: %w", err)
	}
	res.Data = raw
	res.MimeType = mimeFromFormat(format)
	return res, nil
}

func headString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
