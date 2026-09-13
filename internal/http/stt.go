package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// maxSTTAudioBytes mirrors the provider-side cap (openai STT: 25 MB).
	maxSTTAudioBytes int64 = 25 << 20
	// Multipart framing + text fields on top of the audio payload.
	maxSTTBodyBytes = maxSTTAudioBytes + 1<<20
	// Kept small: anything larger than this spills to a temp file instead of RAM.
	sttMultipartMemory   = 4 << 20
	defaultSTTTimeout    = 60 * time.Second
	sttRateLimitRetrySec = "60"
)

// STTHandler serves POST /v1/audio/transcriptions — audio in, text out, via the
// audio.Manager STT chain.
//
// Deliberately NOT wired into the TTS hot-reload path: that path rebuilds the
// manager with setupTTS() only, which would drop every STT provider registered
// by setupAudioExtras. STT config comes from env, so a restart applies changes.
type STTHandler struct {
	manager     *audio.Manager
	rateLimiter func(string) bool // per-IP/token (nil = no limit)
}

// NewSTTHandler creates an STTHandler backed by the given audio.Manager.
func NewSTTHandler(mgr *audio.Manager) *STTHandler {
	return &STTHandler{manager: mgr}
}

// SetRateLimiter injects the server's global limiter.
//
// The key is the bearer token, so every user of a single backend integration
// (e.g. one Drupal gateway token) shares one bucket.
func (h *STTHandler) SetRateLimiter(fn func(string) bool) { h.rateLimiter = fn }

// RegisterRoutes wires the STT endpoint with RoleOperator auth, like TTS.
func (h *STTHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/audio/transcriptions",
		requireAuth(permissions.RoleOperator, h.handleTranscribe))
}

// handleTranscribe accepts multipart fields: file (required), language, model.
func (h *STTHandler) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	if h.rateLimiter != nil {
		key := r.RemoteAddr
		if tok := extractBearerToken(r); tok != "" {
			key = "token:" + tok
		}
		if !h.rateLimiter(key) {
			w.Header().Set("Retry-After", sttRateLimitRetrySec)
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": i18n.T(locale, i18n.MsgRateLimitExceeded)})
			return
		}
	}

	// Checked before reading the body: a missing provider is a deployment gap,
	// and callers must be able to tell it apart from a failed transcription.
	if h.manager == nil || !h.manager.CanTranscribe(ctx) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgSTTNotConfigured)})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSTTBodyBytes)
	if err := r.ParseMultipartForm(sttMultipartMemory); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxSTTAudioBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}
	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	if int64(len(data)) > maxSTTAudioBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	filename := filepath.Base(header.Filename)
	if filename == "." || filename == "/" || strings.Contains(filename, "..") {
		filename = ""
	}

	callCtx, cancel := context.WithTimeout(ctx, defaultSTTTimeout)
	defer cancel()

	res, err := h.manager.Transcribe(callCtx, audio.STTInput{
		Bytes:    data,
		MimeType: header.Header.Get("Content-Type"),
		Filename: filename,
	}, audio.STTOptions{
		Language: strings.TrimSpace(r.FormValue("language")),
		ModelID:  strings.TrimSpace(r.FormValue("model")),
	})
	if err != nil {
		if errors.Is(err, audio.ErrSTTRateLimited) {
			w.Header().Set("Retry-After", sttRateLimitRetrySec)
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": i18n.T(locale, i18n.MsgSTTRateLimited)})
			return
		}
		// Provider errors can echo upstream bodies; log them, never return them.
		slog.Warn("stt.transcribe_failed", "error", err, "bytes", len(data))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": i18n.T(locale, i18n.MsgSTTAllProvidersFailed)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"text":     res.Text,
		"language": res.Language,
		"duration": res.Duration,
		"provider": res.Provider,
	})
}
