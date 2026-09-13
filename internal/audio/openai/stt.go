package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

const (
	sttDefaultAPIBase = "https://api.openai.com/v1"
	sttDefaultModel   = "whisper-1"
	sttDefaultTimeout = 60 * time.Second
	// 25 MB matches the smallest common ceiling among OpenAI-compatible hosts
	// (OpenAI and Groq free tier); rejecting locally beats a remote 413.
	sttMaxBytes = 25 << 20
	// Error bodies are echoed into logs; cap them so a verbose upstream page
	// cannot flood slog.
	sttErrBodyLimit = 512
)

// STTConfig configures an OpenAI-compatible speech-to-text provider.
//
// APIBase is what makes this reusable: Groq, self-hosted faster-whisper
// servers and OpenAI all expose POST {APIBase}/audio/transcriptions.
type STTConfig struct {
	APIKey    string
	APIBase   string // default "https://api.openai.com/v1"
	Model     string // default "whisper-1"
	Language  string // ISO-639-1 hint used when the caller passes none
	TimeoutMs int    // default 60000
}

// STTProvider implements audio.STTProvider against /audio/transcriptions.
type STTProvider struct {
	cfg STTConfig
}

// NewSTTProvider constructs the provider with defaults applied.
func NewSTTProvider(cfg STTConfig) *STTProvider {
	cfg.APIBase = strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if cfg.APIBase == "" {
		cfg.APIBase = sttDefaultAPIBase
	}
	if cfg.Model == "" {
		cfg.Model = sttDefaultModel
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = int(sttDefaultTimeout.Milliseconds())
	}
	return &STTProvider{cfg: cfg}
}

// Name returns the stable provider identifier used in the STT chain.
func (p *STTProvider) Name() string { return "openai" }

// Transcribe sends audio to {APIBase}/audio/transcriptions and returns the text.
// A 429 from the upstream is wrapped with audio.ErrSTTRateLimited.
func (p *STTProvider) Transcribe(ctx context.Context, in audio.STTInput, opts audio.STTOptions) (*audio.TranscriptResult, error) {
	data, filename, err := sttReadInput(in)
	if err != nil {
		return nil, fmt.Errorf("openai stt: %w", err)
	}
	if len(data) > sttMaxBytes {
		return nil, fmt.Errorf("openai stt: file too large (%d bytes, max %d)", len(data), sttMaxBytes)
	}

	model := opts.ModelID
	if model == "" {
		model = p.cfg.Model
	}
	language := opts.Language
	if language == "" {
		language = p.cfg.Language
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := [][2]string{
		{"model", model},
		{"response_format", "json"},
		// Deterministic decoding: dictation must return the same words for the
		// same audio, and higher temperatures invite hallucinated filler.
		{"temperature", "0"},
	}
	if language != "" {
		fields = append(fields, [2]string{"language", language})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return nil, fmt.Errorf("openai stt: write %s field: %w", f[0], err)
		}
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("openai stt: create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return nil, fmt.Errorf("openai stt: write file bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("openai stt: close multipart writer: %w", err)
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.cfg.APIBase+"/audio/transcriptions", &body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai stt: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, sttErrBodyLimit))
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("openai stt: %w: %s", audio.ErrSTTRateLimited, strings.TrimSpace(string(msg)))
		}
		return nil, fmt.Errorf("openai stt: API error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai stt: parse response: %w", err)
	}

	lang := result.Language
	if lang == "" {
		lang = language
	}
	return &audio.TranscriptResult{
		Text:     strings.TrimSpace(result.Text),
		Language: lang,
		Duration: result.Duration,
		Provider: p.Name(),
	}, nil
}

// sttReadInput loads the audio bytes and picks a filename whose extension the
// upstream can sniff — OpenAI-compatible hosts reject unknown extensions even
// when the bytes are valid.
func sttReadInput(in audio.STTInput) ([]byte, string, error) {
	var data []byte
	switch {
	case len(in.Bytes) > 0:
		data = in.Bytes
	case in.FilePath != "":
		info, err := os.Stat(in.FilePath)
		if err != nil {
			return nil, "", fmt.Errorf("stat input: %w", err)
		}
		if info.Size() > sttMaxBytes {
			return nil, "", fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), sttMaxBytes)
		}
		b, err := os.ReadFile(in.FilePath)
		if err != nil {
			return nil, "", fmt.Errorf("read input: %w", err)
		}
		data = b
	default:
		return nil, "", fmt.Errorf("neither FilePath nor Bytes provided")
	}

	name := filepath.Base(in.Filename)
	if name == "" || name == "." || name == "/" {
		name = filepath.Base(in.FilePath)
	}
	if filepath.Ext(name) == "" || name == "." || name == "/" {
		name = "audio" + sttExtFromMime(in.MimeType)
	}
	return data, name, nil
}

// sttExtFromMime maps a MIME type (parameters ignored) to a file extension.
func sttExtFromMime(mime string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	switch base {
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/ogg", "application/ogg":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac":
		return ".m4a"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	default:
		// Groq and OpenAI accept webm; browsers record it by default.
		return ".webm"
	}
}
