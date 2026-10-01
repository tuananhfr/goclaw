package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const studioPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func studioSSE(events ...string) string {
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString("data: " + e + "\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func studioServer(t *testing.T, captured *map[string]any, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if captured != nil {
			_ = json.Unmarshal(raw, captured)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func newStudioProvider(apiBase string) *CodexProvider {
	p := NewCodexProvider("codex-studio", &staticTokenSource{token: "tok"}, apiBase, "gpt-5.6-luna")
	p.retryConfig.Attempts = 1
	return p
}

var okStudioStream = studioSSE(
	`{"type":"response.output_text.delta","delta":"Đã làm ảnh "}`,
	`{"type":"response.output_text.delta","delta":"combo sáng."}`,
	`{"type":"response.output_item.done","item":{"type":"image_generation_call","result":"`+studioPNG+`","output_format":"png","revised_prompt":"A breakfast combo poster","action":"edit"}}`,
	`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5000,"output_tokens":300,"total_tokens":5300}}}`,
)

func TestStudioImage_BuildsRequest(t *testing.T) {
	var body map[string]any
	server := studioServer(t, &body, okStudioStream)
	p := newStudioProvider(server.URL)

	_, err := p.StudioImage(context.Background(), StudioImageRequest{
		ImageModel:   "gpt-image-2.5-flare",
		Quality:      "high",
		Size:         "1024x1360",
		Instructions: "You are the image designer.",
		Text:         "Yêu cầu: làm poster",
		Images:       []ImageContent{{MimeType: "image/png", Data: studioPNG}},
	})
	if err != nil {
		t.Fatalf("StudioImage: %v", err)
	}
	if body["model"] != "gpt-5.6-luna" || body["instructions"] != "You are the image designer." || body["store"] != false {
		t.Fatalf("unexpected top-level body: %v", body)
	}
	if body["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("reasoning effort = %v, want low", body["reasoning"])
	}
	if body["tool_choice"].(map[string]any)["type"] != "image_generation" {
		t.Fatalf("tool_choice = %v", body["tool_choice"])
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["action"] != "auto" || tool["size"] != "1024x1360" || tool["quality"] != "high" || tool["model"] != "gpt-image-2.5-flare" {
		t.Fatalf("tool = %v", tool)
	}
	content := body["input"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "input_image" || content[1].(map[string]any)["type"] != "input_text" {
		t.Fatalf("content order = %v, want image then text", content)
	}
}

func TestStudioImage_DefaultsSizeToAuto(t *testing.T) {
	var body map[string]any
	server := studioServer(t, &body, okStudioStream)
	if _, err := newStudioProvider(server.URL).StudioImage(context.Background(), StudioImageRequest{Text: "x"}); err != nil {
		t.Fatalf("StudioImage: %v", err)
	}
	if got := body["tools"].([]any)[0].(map[string]any)["size"]; got != "auto" {
		t.Fatalf("size = %v, want auto", got)
	}
}

func TestStudioImage_ParsesImageTextPromptActionUsage(t *testing.T) {
	server := studioServer(t, nil, okStudioStream)
	res, err := newStudioProvider(server.URL).StudioImage(context.Background(), StudioImageRequest{Text: "x"})
	if err != nil {
		t.Fatalf("StudioImage: %v", err)
	}
	if len(res.Data) == 0 || res.MimeType != "image/png" {
		t.Fatalf("image not decoded: mime=%q len=%d", res.MimeType, len(res.Data))
	}
	if res.Text != "Đã làm ảnh combo sáng." || res.RevisedPrompt != "A breakfast combo poster" || res.Action != "edit" {
		t.Fatalf("text=%q prompt=%q action=%q", res.Text, res.RevisedPrompt, res.Action)
	}
	if res.Usage == nil || res.Usage.TotalTokens != 5300 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

func TestStudioImage_NoImageErrorCarriesModelText(t *testing.T) {
	stream := studioSSE(`{"type":"response.output_text.delta","delta":"Mình không thể vẽ nội dung này."}`,
		`{"type":"response.completed","response":{"status":"completed"}}`)
	server := studioServer(t, nil, stream)
	_, err := newStudioProvider(server.URL).StudioImage(context.Background(), StudioImageRequest{Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "Mình không thể vẽ nội dung này.") {
		t.Fatalf("err = %v, want it to carry the model text", err)
	}
}

func TestStudioImage_FailedEventIsReported(t *testing.T) {
	stream := studioSSE(`{"type":"response.failed","response":{"status":"failed","error":{"message":"moderation_blocked"}}}`)
	server := studioServer(t, nil, stream)
	_, err := newStudioProvider(server.URL).StudioImage(context.Background(), StudioImageRequest{Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "moderation_blocked") {
		t.Fatalf("err = %v, want moderation_blocked", err)
	}
}

func TestStudioImage_RejectsUnknownImageModel(t *testing.T) {
	server := studioServer(t, nil, okStudioStream)
	_, err := newStudioProvider(server.URL).StudioImage(context.Background(), StudioImageRequest{Text: "x", ImageModel: "dall-e-9"})
	if err == nil || !strings.Contains(err.Error(), "unsupported image model") {
		t.Fatalf("err = %v, want unsupported image model", err)
	}
}

func TestStudioImage_EmptyTextIsRejected(t *testing.T) {
	_, err := newStudioProvider("http://unused").StudioImage(context.Background(), StudioImageRequest{})
	if err == nil {
		t.Fatal("want error for empty text")
	}
}
