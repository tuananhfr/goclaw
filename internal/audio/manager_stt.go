package audio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrAllSTTProvidersFailed is returned when every provider in the chain fails.
var ErrAllSTTProvidersFailed = errors.New("all STT providers failed")

// ErrSTTRateLimited marks a provider rejection for quota/rate limits (HTTP 429).
// Providers wrap it so callers can tell "try later" apart from a broken setup.
var ErrSTTRateLimited = errors.New("stt rate limited")

// defaultSTTChain is the built-in fallback order when no explicit chain is set.
var defaultSTTChain = []string{"elevenlabs", "proxy"}

// Transcribe tries providers in chain order. Returns first success.
// Wraps last error with ErrAllSTTProvidersFailed on total failure.
func (m *Manager) Transcribe(ctx context.Context, in STTInput, opts STTOptions) (*TranscriptResult, error) {
	chain := m.resolveSTTChain(ctx)
	if len(chain) == 0 {
		return nil, fmt.Errorf("%w: chain is empty", ErrAllSTTProvidersFailed)
	}

	var lastErr error
	for _, name := range chain {
		p, ok := m.sttProviders[name]
		if !ok {
			slog.Warn("audio.stt provider not registered, skipping", "provider", name)
			continue
		}
		res, err := p.Transcribe(ctx, in, opts)
		if err == nil {
			return res, nil
		}
		slog.Warn("audio.stt provider failed", "provider", name, "error", err)
		lastErr = err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrAllSTTProvidersFailed, lastErr)
	}
	return nil, fmt.Errorf("%w: no providers matched in chain %v", ErrAllSTTProvidersFailed, chain)
}

// CanTranscribe reports whether the chain resolved for ctx contains at least one
// registered provider. Lets callers answer "not configured" instead of turning a
// setup gap into a generic transcription failure.
func (m *Manager) CanTranscribe(ctx context.Context) bool {
	for _, name := range m.resolveSTTChain(ctx) {
		if _, ok := m.sttProviders[name]; ok {
			return true
		}
	}
	return false
}

// SetSTTChain sets an explicit provider order for STT dispatch.
// Call before first use; not thread-safe after concurrent Transcribe calls.
func (m *Manager) SetSTTChain(chain []string) {
	m.sttChain = chain
}

// RegisterChannelSTT registers STT providers scoped to a specific channel name
// (e.g. "telegram"). Channel-scoped providers take precedence over the manager
// default chain when resolveSTTChain detects a matching channel in ctx.
func (m *Manager) RegisterChannelSTT(channelName string, providers ...STTProvider) {
	if m.channelSTTOverrides == nil {
		m.channelSTTOverrides = make(map[string][]string)
	}
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		m.sttProviders[p.Name()+":"+channelName] = p
		names = append(names, p.Name()+":"+channelName)
	}
	m.channelSTTOverrides[channelName] = names
}

// resolveSTTChain returns the ordered provider names for the current call.
// Precedence: (1) channel override from ctx, (2) explicit m.sttChain,
// (3) defaultSTTChain filtered to registered providers.
func (m *Manager) resolveSTTChain(ctx context.Context) []string {
	// (1) Channel override: check if ctx carries a channel name.
	if ch := channelFromCtx(ctx); ch != "" {
		if overrides, ok := m.channelSTTOverrides[ch]; ok && len(overrides) > 0 {
			return overrides
		}
	}
	// (2) Explicit chain set via SetSTTChain.
	if len(m.sttChain) > 0 {
		return m.sttChain
	}
	// (3) Default: elevenlabs → proxy, filtered to what's registered.
	var out []string
	for _, name := range defaultSTTChain {
		if _, ok := m.sttProviders[name]; ok {
			out = append(out, name)
		}
	}
	return out
}
