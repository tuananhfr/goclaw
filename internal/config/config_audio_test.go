package config

import "testing"

func TestApplySTTEnvOverrides_NoVarsKeepsAudioNil(t *testing.T) {
	for _, k := range []string{"PROVIDER", "API_KEY", "BASE_URL", "MODEL", "LANGUAGE"} {
		t.Setenv("GOCLAW_AUDIO_STT_"+k, "")
	}
	c := &Config{}
	c.applySTTEnvOverrides()
	if c.Audio != nil {
		t.Fatalf("Audio = %+v, want nil when no GOCLAW_AUDIO_STT_* is set", c.Audio)
	}
}

func TestApplySTTEnvOverrides_AllocatesAndOverlays(t *testing.T) {
	t.Setenv("GOCLAW_AUDIO_STT_PROVIDER", "openai")
	t.Setenv("GOCLAW_AUDIO_STT_API_KEY", "gsk_env")
	t.Setenv("GOCLAW_AUDIO_STT_BASE_URL", "https://api.groq.com/openai/v1")
	t.Setenv("GOCLAW_AUDIO_STT_MODEL", "whisper-large-v3")
	t.Setenv("GOCLAW_AUDIO_STT_LANGUAGE", "vi")

	c := &Config{}
	c.applySTTEnvOverrides()
	if c.Audio == nil || c.Audio.Stt == nil {
		t.Fatal("Audio.Stt not allocated")
	}
	s := c.Audio.Stt
	if s.Provider != "openai" || s.APIKey != "gsk_env" || s.BaseURL != "https://api.groq.com/openai/v1" || s.Model != "whisper-large-v3" || s.Language != "vi" {
		t.Errorf("Stt = %+v", s)
	}
}

func TestApplySTTEnvOverrides_KeepsFileValuesForUnsetVars(t *testing.T) {
	t.Setenv("GOCLAW_AUDIO_STT_PROVIDER", "")
	t.Setenv("GOCLAW_AUDIO_STT_BASE_URL", "")
	t.Setenv("GOCLAW_AUDIO_STT_MODEL", "")
	t.Setenv("GOCLAW_AUDIO_STT_LANGUAGE", "")
	t.Setenv("GOCLAW_AUDIO_STT_API_KEY", "gsk_env")

	c := &Config{Audio: &AudioConfig{Stt: &AudioSTTConfig{Provider: "openai", Model: "whisper-large-v3-turbo"}}}
	c.applySTTEnvOverrides()
	if c.Audio.Stt.Provider != "openai" || c.Audio.Stt.Model != "whisper-large-v3-turbo" {
		t.Errorf("file values overwritten: %+v", c.Audio.Stt)
	}
	if c.Audio.Stt.APIKey != "gsk_env" {
		t.Errorf("APIKey = %q, want env value", c.Audio.Stt.APIKey)
	}
}

func TestMaskedCopy_MasksSTTAPIKey(t *testing.T) {
	c := Default()
	c.Audio = &AudioConfig{Stt: &AudioSTTConfig{Provider: "openai", APIKey: "gsk_secret"}}
	cp := c.MaskedCopy()
	if cp.Audio == nil || cp.Audio.Stt == nil {
		t.Fatal("masked copy lost Audio.Stt")
	}
	if cp.Audio.Stt.APIKey == "gsk_secret" {
		t.Error("STT API key leaked through MaskedCopy")
	}
	if c.Audio.Stt.APIKey != "gsk_secret" {
		t.Error("MaskedCopy mutated the original config")
	}
}
