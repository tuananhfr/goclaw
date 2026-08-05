package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// invalidFcIDChars matches characters not allowed in Responses API tool call IDs.
var invalidFcIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// buildRequestBody converts internal ChatRequest to Responses API format.
func (p *CodexProvider) buildRequestBody(req ChatRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var instructions string
	var input []any
	seenFunctionCalls := make(map[string]struct{})

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if instructions == "" {
				instructions = m.Content
			} else {
				instructions += "\n\n" + m.Content
			}

		case "user":
			if len(m.Images) > 0 {
				var parts []map[string]any
				for _, img := range m.Images {
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data),
					})
				}
				if m.Content != "" {
					parts = append(parts, map[string]any{
						"type": "input_text",
						"text": m.Content,
					})
				}
				input = append(input, map[string]any{
					"role":    "user",
					"content": parts,
				})
			} else {
				input = append(input, map[string]any{
					"role":    "user",
					"content": m.Content,
				})
			}

		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					callID := toFcID(tc.ID)
					seenFunctionCalls[callID] = struct{}{}
					input = append(input, map[string]any{
						"type":      "function_call",
						"id":        callID,
						"call_id":   callID,
						"name":      tc.Name,
						"arguments": string(argsJSON),
					})
				}
			}
			if m.Content != "" {
				item := map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": m.Content},
					},
				}
				if m.Phase != "" {
					item["phase"] = m.Phase
				}
				input = append(input, item)
			}

		case "tool":
			callID := toFcID(m.ToolCallID)
			if _, ok := seenFunctionCalls[callID]; !ok {
				// Responses API rejects orphaned function_call_output items with:
				// "No tool call found for function call output with call_id ...".
				// Session repair usually removes these, but provider serialization
				// is the last boundary before the HTTP request and must not emit
				// structurally invalid input.
				continue
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  m.Content,
			})
		}
	}

	body := map[string]any{
		"model":  model,
		"input":  input,
		"stream": stream,
		"store":  false,
	}

	if instructions == "" {
		instructions = "You are a helpful assistant."
	}
	body["instructions"] = instructions

	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			if t.Type == "image_generation" {
				// Pass native image_generation tool object as-is — Responses API first-class tool.
				// Defaults chosen for Phase 1b; per-agent overrides are Phase 4.
				tools = append(tools, map[string]any{
					"type":           "image_generation",
					"action":         "generate",
					"model":          "gpt-image-2",
					"output_format":  "png",
					"partial_images": 1,
				})
			} else {
				// Function tool path (default). Works with both value-type and pointer Function
				// fields — we only access t.Function when type is not "image_generation".
				tools = append(tools, map[string]any{
					"type":        "function",
					"name":        t.Function.Name,
					"description": t.Function.Description,
					"parameters":  NormalizeSchema("codex", t.Function.Parameters),
				})
			}
		}
		body["tools"] = tools
		if choice := codexToolChoice(req.ToolChoice); choice != nil {
			body["tool_choice"] = choice
		}
	}

	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		body["reasoning"] = map[string]any{"effort": level}
	}

	if fast, ok := req.Options[OptFastMode].(bool); ok && fast && codexFastTierSupported(model) {
		// Fast mode: ~1.5x speed at a higher credit burn. The config-facing name
		// is "fast" but the wire value is "priority" — Codex CLI's
		// ServiceTier::Fast.request_value() maps to "priority", and the ChatGPT
		// backend rejects a literal "fast". Set here (not via middleware) so both
		// CodexProvider.ChatStream and CodexAdapter.ToRequest pick it up.
		body["service_tier"] = "priority"
	}

	return body
}

// codexFastTierSupported mirrors the Codex CLI model catalog: fast tier is
// advertised only by the gpt-5.4 / gpt-5.5 / gpt-5.6 families. Unsupported
// models must not send service_tier — the CLI strips it the same way.
func codexFastTierSupported(model string) bool {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	for _, prefix := range []string{"gpt-5.4", "gpt-5.5", "gpt-5.6"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

func codexToolChoice(choice *ToolChoice) any {
	if choice == nil {
		return nil
	}
	switch choice.Mode {
	case "function":
		if choice.Name == "" {
			return nil
		}
		return map[string]any{
			"type": "function",
			"name": choice.Name,
		}
	case "auto", "required", "none":
		return choice.Mode
	default:
		return nil
	}
}

func (p *CodexProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	endpoint := p.apiBase + "/codex/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: get auth token: %w", p.name, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("OpenAI-Beta", "responses=v1")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{
			Status:     resp.StatusCode,
			Body:       fmt.Sprintf("%s: %s", p.name, string(respBody)),
			RetryAfter: retryAfter,
		}
	}

	return resp.Body, nil
}

// toFcID ensures a tool call ID starts with "fc_" and contains only
// letters, numbers, underscores, or dashes as required by the Responses API.
func toFcID(id string) string {
	if strings.HasPrefix(id, "tool_") {
		id = id[len("tool_"):]
	} else if strings.HasPrefix(id, "call_") {
		id = id[len("call_"):]
	} else if strings.HasPrefix(id, "fc_") {
		id = id[len("fc_"):]
	}
	// Replace invalid characters (e.g. colons from session keys) with underscores.
	id = invalidFcIDChars.ReplaceAllString(id, "_")
	return "fc_" + id
}
