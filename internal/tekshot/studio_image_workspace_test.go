package tekshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestStudioImageWorkspaceStoresUnderConfiguredAgent(t *testing.T) {
	server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
	s, root := newStudioService(t, server, nil)
	paths := make(map[string]string)
	for _, key := range []string{"designer", "content-creator"} {
		target := filepath.Join(root, "custom-agents", key)
		agents := &imageTraceAgentStore{id: uuid.New(), workspace: target}
		s.studio.Agents = agents
		job := &store.TekshotJob{ID: uuid.New(), AgentKey: key}
		out, _, err := s.runStudioImage(context.Background(), job, studioDrawArgs())
		if err != nil {
			t.Fatal(err)
		}
		media := out.(map[string]any)["media"].([]agent.MediaResult)
		if len(media) != 1 || !strings.HasPrefix(media[0].Path, filepath.Join(target, "tekshot_studio")+string(filepath.Separator)) {
			t.Fatalf("media = %+v", media)
		}
		if _, err := os.Stat(media[0].Path); err != nil {
			t.Fatal(err)
		}
		if agents.calls != 1 {
			t.Fatalf("agent lookup count = %d", agents.calls)
		}
		paths[key] = media[0].Path
	}
	if paths["designer"] == paths["content-creator"] {
		t.Fatal("agent output paths overlap")
	}
}

func TestStudioImageWorkspaceFailuresDoNotCallModel(t *testing.T) {
	for _, name := range []string{"empty", "outside", "shared_root", "file", "lookup", "missing_store", "symlink", "output_symlink", "date_symlink"} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				_, _ = io.WriteString(w, imageTraceStream())
			})
			s, root := newStudioService(t, server, nil)
			f := newImageTraceFixture(t, s)
			agents := s.studio.Agents.(*imageTraceAgentStore)
			switch name {
			case "empty":
				agents.workspace = ""
			case "outside":
				agents.workspace = t.TempDir()
			case "shared_root":
				agents.workspace = root
			case "file":
				if err := os.WriteFile(agents.workspace, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			case "lookup":
				agents.err = errors.New("agent lookup failed")
			case "missing_store":
				s.studio.Agents = nil
			case "symlink":
				if err := os.Symlink(t.TempDir(), agents.workspace); err != nil {
					t.Fatal(err)
				}
			case "output_symlink", "date_symlink":
				link := filepath.Join(agents.workspace, "tekshot_studio")
				if name == "date_symlink" {
					link = filepath.Join(link, time.Now().Format("2006-01-02"))
				}
				if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.process(t, context.Background(), studioDrawArgs()); err == nil {
				t.Fatal("want invalid workspace error")
			}
			trace, spans := f.snapshot(t)
			if hits.Load() != 0 || trace.Status != store.TraceStatusError || trace.Error == "" {
				t.Fatalf("hits=%d trace=%+v", hits.Load(), trace)
			}
			for _, span := range spans {
				if span.SpanType == store.SpanTypeLLMCall {
					t.Fatal("invalid workspace reached model")
				}
			}
		})
	}
}

func TestStudioImageWorkspaceTraceReusesAgentLookup(t *testing.T) {
	server := imageTraceServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, imageTraceStream()) })
	s, _ := newStudioService(t, server, nil)
	f := newImageTraceFixture(t, s)
	agents := s.studio.Agents.(*imageTraceAgentStore)
	if err := f.process(t, context.Background(), studioDrawArgs()); err != nil {
		t.Fatal(err)
	}
	trace, spans := f.snapshot(t)
	if agents.calls != 1 || trace.AgentID == nil || *trace.AgentID != agents.id {
		t.Fatalf("calls=%d trace=%+v", agents.calls, trace)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(trace.OutputPreview), &output); err != nil {
		t.Fatal(err)
	}
	media := output["media"].([]any)
	path := media[0].(map[string]any)["path"].(string)
	if !strings.HasPrefix(path, agents.workspace+string(filepath.Separator)) {
		t.Fatalf("output=%s", trace.OutputPreview)
	}
	for _, span := range spans {
		if span.AgentID == nil || *span.AgentID != agents.id {
			t.Fatalf("span agent = %+v", span)
		}
		if span.Name == "Save generated image" {
			var saved map[string]any
			if err := json.Unmarshal([]byte(span.OutputPreview), &saved); err != nil {
				t.Fatal(err)
			}
			if saved["path"] != path {
				t.Fatalf("save span = %+v", span)
			}
		}
	}
}

func TestStudioImageWorkspaceCanEditLegacyImage(t *testing.T) {
	server := imageTraceServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(string(body), studioTestPNG) {
			t.Error("legacy input image missing from model request")
		}
		_, _ = io.WriteString(w, imageTraceStream())
	})
	s, root := newStudioService(t, server, nil)
	legacy := filepath.Join(root, "tekshot_studio", "2026-09-30", "legacy.png")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(studioTestPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0644); err != nil {
		t.Fatal(err)
	}
	args := studioDrawArgs()
	args["media"] = []any{map[string]any{"path": legacy, "role": "base"}}
	out, _, err := s.runStudioImage(context.Background(), &store.TekshotJob{ID: uuid.New(), AgentKey: "designer"}, args)
	if err != nil {
		t.Fatal(err)
	}
	media := out.(map[string]any)["media"].([]agent.MediaResult)
	if !strings.HasPrefix(media[0].Path, filepath.Join(root, "designer", "tekshot_studio")) {
		t.Fatalf("new image path=%s", media[0].Path)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy image changed: %v", err)
	}
}
