package http

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

const sttTestToken = "stt-test-token"

type mockSTTProvider struct {
	name    string
	gotIn   audio.STTInput
	gotOpts audio.STTOptions
	result  *audio.TranscriptResult
	err     error
}

func (m *mockSTTProvider) Name() string { return m.name }

func (m *mockSTTProvider) Transcribe(_ context.Context, in audio.STTInput, opts audio.STTOptions) (*audio.TranscriptResult, error) {
	m.gotIn, m.gotOpts = in, opts
	return m.result, m.err
}

func newSTTMux(mgr *audio.Manager) *http.ServeMux {
	mux := http.NewServeMux()
	NewSTTHandler(mgr).RegisterRoutes(mux)
	return mux
}

func sttManagerWith(p *mockSTTProvider) *audio.Manager {
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterSTT(p)
	mgr.SetSTTChain([]string{p.name})
	return mgr
}

// sttMultipart builds a request body; omit file by passing nil audio.
func sttMultipart(t *testing.T, audioBytes []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if audioBytes != nil {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="file"; filename="dictation.webm"`)
		h.Set("Content-Type", "audio/webm;codecs=opus")
		fw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(audioBytes)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func doSTT(t *testing.T, mux *http.ServeMux, body *bytes.Buffer, contentType, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", contentType)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestTranscribe_Unauthenticated(t *testing.T) {
	setupTestToken(t, sttTestToken)
	mux := newSTTMux(sttManagerWith(&mockSTTProvider{name: "openai", result: &audio.TranscriptResult{Text: "x"}}))

	body, ct := sttMultipart(t, []byte("audio"), nil)
	if rr := doSTT(t, mux, body, ct, ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestTranscribe_NotConfigured(t *testing.T) {
	setupTestToken(t, "")
	mux := newSTTMux(audio.NewManager(audio.ManagerConfig{}))

	body, ct := sttMultipart(t, []byte("audio"), nil)
	if rr := doSTT(t, mux, body, ct, ""); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestTranscribe_MissingFile(t *testing.T) {
	setupTestToken(t, "")
	mux := newSTTMux(sttManagerWith(&mockSTTProvider{name: "openai", result: &audio.TranscriptResult{Text: "x"}}))

	body, ct := sttMultipart(t, nil, map[string]string{"language": "vi"})
	if rr := doSTT(t, mux, body, ct, ""); rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestTranscribe_Success(t *testing.T) {
	setupTestToken(t, sttTestToken)
	p := &mockSTTProvider{name: "openai", result: &audio.TranscriptResult{Text: "cho tôi xem tờ trình", Language: "vi", Duration: 3.2, Provider: "openai"}}
	mux := newSTTMux(sttManagerWith(p))

	body, ct := sttMultipart(t, []byte("webm-bytes"), map[string]string{"language": "vi", "model": "whisper-large-v3"})
	rr := doSTT(t, mux, body, ct, sttTestToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"text":"cho tôi xem tờ trình"`) {
		t.Errorf("body = %s", rr.Body.String())
	}
	if string(p.gotIn.Bytes) != "webm-bytes" || p.gotIn.Filename != "dictation.webm" || p.gotIn.MimeType != "audio/webm;codecs=opus" {
		t.Errorf("provider input = %+v", p.gotIn)
	}
	if p.gotOpts.Language != "vi" || p.gotOpts.ModelID != "whisper-large-v3" {
		t.Errorf("provider opts = %+v", p.gotOpts)
	}
}

func TestTranscribe_ProviderRateLimitedMapsTo429(t *testing.T) {
	setupTestToken(t, "")
	p := &mockSTTProvider{name: "openai", err: fmt.Errorf("openai stt: %w: quota", audio.ErrSTTRateLimited)}
	mux := newSTTMux(sttManagerWith(p))

	body, ct := sttMultipart(t, []byte("audio"), nil)
	rr := doSTT(t, mux, body, ct, "")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
}

func TestTranscribe_ProviderErrorIsNotLeaked(t *testing.T) {
	setupTestToken(t, "")
	p := &mockSTTProvider{name: "openai", err: fmt.Errorf("openai stt: API error 401: Invalid API Key gsk_leaky")}
	mux := newSTTMux(sttManagerWith(p))

	body, ct := sttMultipart(t, []byte("audio"), nil)
	rr := doSTT(t, mux, body, ct, "")
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", rr.Code, rr.Body.String())
	}
	// An upstream 401 must never surface as 401 (callers treat that as their
	// own auth failure) nor echo upstream text.
	if strings.Contains(rr.Body.String(), "gsk_leaky") || strings.Contains(rr.Body.String(), "Invalid API Key") {
		t.Errorf("upstream error leaked: %s", rr.Body.String())
	}
}
