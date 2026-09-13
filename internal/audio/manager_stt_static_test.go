package audio

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestManager_CanTranscribe(t *testing.T) {
	m := newTestManager()
	if m.CanTranscribe(context.Background()) {
		t.Fatal("fresh manager must not report STT as available")
	}

	// A chain naming only unregistered providers is still "not configured".
	m.SetSTTChain([]string{"openai"})
	if m.CanTranscribe(context.Background()) {
		t.Fatal("chain with unregistered provider must not count as available")
	}

	m.RegisterSTT(&mockSTT{name: "openai", result: &TranscriptResult{Text: "ok"}})
	if !m.CanTranscribe(context.Background()) {
		t.Fatal("registered provider in chain must count as available")
	}
}

// A registered provider outside the default chain is ignored unless the chain
// names it — the reason setupStaticSTT must call SetSTTChain.
func TestManager_CanTranscribe_RegisteredButNotInDefaultChain(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{name: "openai", result: &TranscriptResult{Text: "ok"}})
	if m.CanTranscribe(context.Background()) {
		t.Fatal("provider absent from default chain must not be used")
	}
}

func TestManager_Transcribe_RateLimitSurvivesWrapping(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{name: "openai", err: fmt.Errorf("openai stt: %w: quota", ErrSTTRateLimited)})
	m.SetSTTChain([]string{"openai"})

	_, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if !errors.Is(err, ErrAllSTTProvidersFailed) {
		t.Errorf("err = %v, want ErrAllSTTProvidersFailed", err)
	}
	if !errors.Is(err, ErrSTTRateLimited) {
		t.Errorf("err = %v, want ErrSTTRateLimited still detectable through manager wrapping", err)
	}
}
