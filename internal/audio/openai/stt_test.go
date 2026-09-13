package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// capturedSTTRequest records what the provider actually put on the wire.
type capturedSTTRequest struct {
	path     string
	auth     string
	fields   map[string]string
	filename string
	fileData string
}

func newTestSTT(t *testing.T, cfg STTConfig, handler func(w http.ResponseWriter, got *capturedSTTRequest)) (*STTProvider, *capturedSTTRequest) {
	t.Helper()
	got := &capturedSTTRequest{fields: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for k, v := range r.MultipartForm.Value {
			got.fields[k] = v[0]
		}
		if f, h, err := r.FormFile("file"); err == nil {
			got.filename = h.Filename
			b, _ := io.ReadAll(f)
			got.fileData = string(b)
			f.Close()
		}
		handler(w, got)
	}))
	t.Cleanup(srv.Close)
	cfg.APIBase = srv.URL + "/openai/v1"
	return NewSTTProvider(cfg), got
}

func TestSTTProvider_SendsOpenAICompatibleRequest(t *testing.T) {
	p, got := newTestSTT(t, STTConfig{APIKey: "gsk_test", Model: "whisper-large-v3", Language: "vi"},
		func(w http.ResponseWriter, _ *capturedSTTRequest) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"text":"  cho tôi xem các tờ trình tạm ứng  "}`)
		})

	res, err := p.Transcribe(context.Background(), audio.STTInput{
		Bytes:    []byte("fake-webm-bytes"),
		MimeType: "audio/webm;codecs=opus",
	}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if got.path != "/openai/v1/audio/transcriptions" {
		t.Errorf("path = %q, want /openai/v1/audio/transcriptions", got.path)
	}
	if got.auth != "Bearer gsk_test" {
		t.Errorf("Authorization = %q", got.auth)
	}
	for k, want := range map[string]string{"model": "whisper-large-v3", "language": "vi", "response_format": "json", "temperature": "0"} {
		if got.fields[k] != want {
			t.Errorf("field %s = %q, want %q", k, got.fields[k], want)
		}
	}
	// Codec parameters must not leak into the extension, or the upstream
	// cannot sniff the format.
	if got.filename != "audio.webm" {
		t.Errorf("filename = %q, want audio.webm", got.filename)
	}
	if got.fileData != "fake-webm-bytes" {
		t.Errorf("file bytes = %q", got.fileData)
	}
	if res.Text != "cho tôi xem các tờ trình tạm ứng" {
		t.Errorf("Text = %q (want trimmed)", res.Text)
	}
	if res.Provider != "openai" || res.Language != "vi" {
		t.Errorf("Provider/Language = %q/%q", res.Provider, res.Language)
	}
}

func TestSTTProvider_OptionsOverrideConfig(t *testing.T) {
	p, got := newTestSTT(t, STTConfig{APIKey: "k", Model: "whisper-large-v3", Language: "vi"},
		func(w http.ResponseWriter, _ *capturedSTTRequest) {
			_, _ = io.WriteString(w, `{"text":"hello"}`)
		})

	_, err := p.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("x"), Filename: "clip.m4a"},
		audio.STTOptions{Language: "en", ModelID: "whisper-large-v3-turbo"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got.fields["language"] != "en" || got.fields["model"] != "whisper-large-v3-turbo" {
		t.Errorf("fields = %v, want per-call overrides", got.fields)
	}
	if got.filename != "clip.m4a" {
		t.Errorf("filename = %q, want caller filename kept", got.filename)
	}
}

func TestSTTProvider_RateLimitIsDistinguishable(t *testing.T) {
	p, _ := newTestSTT(t, STTConfig{APIKey: "k"},
		func(w http.ResponseWriter, _ *capturedSTTRequest) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"Rate limit reached"}}`)
		})

	_, err := p.Transcribe(context.Background(), audio.STTInput{Bytes: []byte("x")}, audio.STTOptions{})
	if !errors.Is(err, audio.ErrSTTRateLimited) {
		t.Fatalf("err = %v, want errors.Is ErrSTTRateLimited", err)
	}
}

func TestSTTProvider_OtherErrorsAreNotRateLimit(t *testing.T) {
	p, _ := newTestSTT(t, STTConfig{APIKey: "bad"},
		func(w http.ResponseWriter, _ *capturedSTTRequest) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Invalid API Key"}}`)
		})

	_, err := p.Transcribe(context.Background(), audio.STTInput{Bytes: []byte("x")}, audio.STTOptions{})
	if err == nil || errors.Is(err, audio.ErrSTTRateLimited) {
		t.Fatalf("err = %v, want non-rate-limit error", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want status code in message", err)
	}
}

func TestSTTProvider_RejectsOversizeBeforeUpload(t *testing.T) {
	called := false
	p, _ := newTestSTT(t, STTConfig{APIKey: "k"},
		func(w http.ResponseWriter, _ *capturedSTTRequest) { called = true })

	_, err := p.Transcribe(context.Background(), audio.STTInput{Bytes: make([]byte, sttMaxBytes+1)}, audio.STTOptions{})
	if err == nil {
		t.Fatal("want error for oversize input")
	}
	if called {
		t.Error("oversize audio must be rejected locally, not uploaded")
	}
}

func TestSTTProvider_Defaults(t *testing.T) {
	p := NewSTTProvider(STTConfig{APIBase: "https://api.groq.com/openai/v1/"})
	if p.cfg.APIBase != "https://api.groq.com/openai/v1" {
		t.Errorf("APIBase = %q, want trailing slash trimmed", p.cfg.APIBase)
	}
	if p.cfg.Model != sttDefaultModel || p.cfg.TimeoutMs <= 0 {
		t.Errorf("defaults not applied: %+v", p.cfg)
	}
	if NewSTTProvider(STTConfig{}).cfg.APIBase != sttDefaultAPIBase {
		t.Error("empty APIBase must fall back to OpenAI")
	}
}
