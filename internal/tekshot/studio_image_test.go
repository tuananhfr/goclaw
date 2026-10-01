package tekshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/providertest"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const studioTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

type fakeBuiltinTools struct {
	store.BuiltinToolStore
	settings json.RawMessage
}

func (f fakeBuiltinTools) GetSettings(context.Context, string) (json.RawMessage, error) {
	return f.settings, nil
}

type fakeSkills struct {
	store.SkillStore
	skills map[string]string
}

func (f fakeSkills) LoadSkill(_ context.Context, name string) (string, bool) {
	content, ok := f.skills[name]
	return content, ok
}

// studioCodexServer answers the image call with an image, a text-only call
// (the library pick) with pickReply, and GET /ref.png with a PNG.
func studioCodexServer(t *testing.T, pickReply string, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	png, _ := base64.StdEncoding.DecodeString(studioTestPNG)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/ref.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if _, isImage := body["tool_choice"]; isImage {
			_, _ = w.Write([]byte(
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Đã tạo poster.\"}\n\n" +
					"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"" + studioTestPNG + "\",\"output_format\":\"png\",\"revised_prompt\":\"poster\",\"action\":\"generate\"}}\n\n" +
					"data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":" + mustJSON(pickReply) + "}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":" + mustJSON(pickReply) + "}]}]}}\n\n" +
				"data: [DONE]\n\n"))
	}))
	t.Cleanup(s.Close)
	return s
}

func mustJSON(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func newStudioService(t *testing.T, server *httptest.Server, skills map[string]string) (*JobService, string) {
	t.Helper()
	registry := providers.NewRegistry(nil)
	registry.Register(providertest.NewCodexProviderFast("openai-codex", server.URL))
	workspace := t.TempDir()
	s := &JobService{httpClient: server.Client()}
	s.SetStudioImageDeps(StudioImageDeps{
		Providers: registry,
		BuiltinTools: fakeBuiltinTools{settings: json.RawMessage(
			`{"providers":[{"provider":"openai-codex","model":"gpt-5.6-luna","enabled":true,"params":{"image_model":"gpt-image-2.5-flare","quality":"high"}}]}`)},
		Skills:    fakeSkills{skills: skills},
		Workspace: workspace,
	})
	return s, workspace
}

func writeTempPNG(t *testing.T) string {
	t.Helper()
	png, _ := base64.StdEncoding.DecodeString(studioTestPNG)
	path := filepath.Join(t.TempDir(), "base.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunStudioImage_DrawsOnceAndWritesPNG(t *testing.T) {
	var bodies []map[string]any
	server := studioCodexServer(t, `{"id": 0}`, &bodies)
	s, workspace := newStudioService(t, server, map[string]string{"poster": "Dùng chữ to."})
	job := &store.TekshotJob{ID: uuid.New(), JobType: TekshotJobTypeStudioImage}

	out, progress, err := s.runStudioImage(context.Background(), job, map[string]any{
		"instructions":  "You are the image designer.",
		"prompt":        "Yêu cầu: làm poster",
		"size":          "1024x1360",
		"media":         []any{map[string]any{"path": writeTempPNG(t), "role": "base"}},
		"tagged_skills": []any{"poster", "missing-skill"},
	})
	if err != nil {
		t.Fatalf("runStudioImage: %v", err)
	}
	if progress != "Completed" || len(bodies) != 1 {
		t.Fatalf("progress=%q calls=%d, want Completed and exactly one draw", progress, len(bodies))
	}
	result := out.(map[string]any)
	if result["content"] != "Đã tạo poster." || result["action"] != "generate" || result["reference_image_id"] != 0 {
		t.Fatalf("result = %+v", result)
	}
	media := result["media"].([]agent.MediaResult)
	if len(media) != 1 || !strings.HasPrefix(media[0].Path, filepath.Join(workspace, "tekshot_studio")) || media[0].Prompt != "poster" {
		t.Fatalf("media = %+v", media)
	}
	if _, err := os.Stat(media[0].Path); err != nil {
		t.Fatalf("PNG not written: %v", err)
	}
	text := bodies[0]["input"].([]any)[0].(map[string]any)["content"].([]any)[1].(map[string]any)["text"].(string)
	if !strings.Contains(text, "### poster\nDùng chữ to.") || strings.Contains(text, "missing-skill") {
		t.Fatalf("prompt text = %q", text)
	}
}

func TestRunStudioImage_LibraryPickAttachesImageLast(t *testing.T) {
	var bodies []map[string]any
	server := studioCodexServer(t, `{"id": 12}`, &bodies)
	s, _ := newStudioService(t, server, nil)
	job := &store.TekshotJob{ID: uuid.New(), JobType: TekshotJobTypeStudioImage}

	out, _, err := s.runStudioImage(context.Background(), job, map[string]any{
		"prompt": "Yêu cầu: ảnh combo",
		"reference_library": []any{
			map[string]any{"id": 12, "url": server.URL + "/ref.png", "description": "Combo bánh mì trên bàn gỗ"},
		},
	})
	if err != nil {
		t.Fatalf("runStudioImage: %v", err)
	}
	if out.(map[string]any)["reference_image_id"] != 12 {
		t.Fatalf("reference_image_id = %v", out.(map[string]any)["reference_image_id"])
	}
	if len(bodies) != 2 {
		t.Fatalf("calls = %d, want pick + draw", len(bodies))
	}
	content := bodies[1]["input"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || !strings.Contains(content[1].(map[string]any)["text"].(string), "Ảnh kho (đính kèm cuối): Combo bánh mì") {
		t.Fatalf("draw content = %v", content)
	}
}

func TestRunStudioImage_RejectsMediaOutsideAllowedDirs(t *testing.T) {
	var bodies []map[string]any
	server := studioCodexServer(t, `{"id": 0}`, &bodies)
	s, _ := newStudioService(t, server, nil)
	_, _, err := s.runStudioImage(context.Background(), &store.TekshotJob{ID: uuid.New()}, map[string]any{
		"prompt": "x",
		"media":  []any{map[string]any{"path": "/etc/passwd", "role": "reference"}},
	})
	if err == nil || len(bodies) != 0 {
		t.Fatalf("err=%v calls=%d, want rejection before any call", err, len(bodies))
	}
}

func TestRunStudioImage_NotWired(t *testing.T) {
	if _, _, err := (&JobService{}).runStudioImage(context.Background(), &store.TekshotJob{}, map[string]any{"prompt": "x"}); err == nil {
		t.Fatal("want error when deps are not wired")
	}
}

func TestCapStudioImagesKeepsBaseFirst(t *testing.T) {
	images := []providers.ImageContent{{Data: "base"}, {Data: "r1"}, {Data: "r2"}, {Data: "r3"}, {Data: "r4"}}
	got := capStudioImages(images, 3)
	if len(got) != 3 || got[0].Data != "base" || got[2].Data != "r2" {
		t.Fatalf("got %+v", got)
	}
}

// studioFailServer answers every draw with status (or, when status is 200, a
// text-only refusal) and counts the draws.
func studioFailServer(t *testing.T, status int, draws *int) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*draws++
		if status != http.StatusOK {
			http.Error(w, "upstream busy", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Mình không thể vẽ nội dung này.\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n" +
				"data: [DONE]\n\n"))
	}))
	t.Cleanup(s.Close)
	return s
}

func newStudioServiceWithChain(t *testing.T, server *httptest.Server, chain string) *JobService {
	t.Helper()
	registry := providers.NewRegistry(nil)
	registry.Register(providertest.NewCodexProviderFast("openai-codex", server.URL))
	s := &JobService{httpClient: server.Client()}
	s.SetStudioImageDeps(StudioImageDeps{
		Providers:    registry,
		BuiltinTools: fakeBuiltinTools{settings: json.RawMessage(`{"providers":` + chain + `}`)},
		Skills:       fakeSkills{},
		Workspace:    t.TempDir(),
	})
	return s
}

func studioDrawArgs() map[string]any {
	return map[string]any{"instructions": "x", "prompt": "Yêu cầu: làm poster", "size": "auto"}
}

const codexEntry = `{"provider":"openai-codex","model":"gpt-5.6-luna","enabled":true,"max_retries":2,"params":{"image_model":"gpt-image-2.5-flare"}}`

func TestRunStudioImage_RefusalStopsTheChainAndShowsTheModelText(t *testing.T) {
	draws := 0
	server := studioFailServer(t, http.StatusOK, &draws)
	// Second Codex entry + a fallback that does not exist: neither may redraw
	// nor replace the model's own words.
	s := newStudioServiceWithChain(t, server, `[`+codexEntry+`,`+codexEntry+`,{"provider":"gemini","model":"g","enabled":true}]`)
	job := &store.TekshotJob{ID: uuid.New(), JobType: TekshotJobTypeStudioImage}

	_, _, err := s.runStudioImage(context.Background(), job, studioDrawArgs())

	if err == nil || err.Error() != "Mình không thể vẽ nội dung này." {
		t.Fatalf("err = %v, want exactly the model text", err)
	}
	if draws != 1 {
		t.Fatalf("draws = %d, want 1 (a refusal must not be redrawn)", draws)
	}
}

func TestRunStudioImage_OverloadShowsTheRetryLaterMessage(t *testing.T) {
	draws := 0
	server := studioFailServer(t, http.StatusServiceUnavailable, &draws)
	s := newStudioServiceWithChain(t, server, `[`+codexEntry+`,{"provider":"gemini","model":"g","enabled":true}]`)
	job := &store.TekshotJob{ID: uuid.New(), JobType: TekshotJobTypeStudioImage}

	_, _, err := s.runStudioImage(context.Background(), job, studioDrawArgs())

	if err == nil || err.Error() != "Máy vẽ đang quá tải, thử lại sau ít phút." {
		t.Fatalf("err = %v, want the overload message", err)
	}
}
