package config

import "os"

// AudioConfig is the optional cfg.Audio block for STT/Music static defaults.
// Pointer-typed field on Config so absent/empty JSON silently decodes as nil —
// existing config.json files that predate Phase 1 continue to load unchanged.
//
// Phase 1 ships the shape only (no consumers yet). Phase 3 wires Music; Phase 4
// wires STT. In both phases the Manager continues to honor per-tenant
// builtin_tools settings regardless of whether cfg.Audio is set.
type AudioConfig struct {
	Stt   *AudioSTTConfig   `json:"stt,omitempty"`
	Music *AudioMusicConfig `json:"music,omitempty"`
}

// AudioSTTConfig configures optional static STT defaults. Empty = "no global
// default; rely on tenant builtin_tools[stt] or per-channel STTProxyURL".
type AudioSTTConfig struct {
	Provider   string `json:"provider,omitempty"`    // primary provider name (e.g. "scribe")
	APIKey     string `json:"api_key,omitempty"`     // may be overridden by env / secrets
	BaseURL    string `json:"base_url,omitempty"`    // override for enterprise deploys
	Model      string `json:"model,omitempty"`       // provider-specific
	Language   string `json:"language,omitempty"`    // BCP-47 hint
	Fallback   string `json:"fallback,omitempty"`    // provider name to try on primary failure
	TimeoutMs  int    `json:"timeout_ms,omitempty"`  // default 30000
}

// applySTTEnvOverrides overlays GOCLAW_AUDIO_STT_* env vars onto cfg.Audio.Stt.
//
// The block is allocated only when at least one var is set: consumers treat a
// nil cfg.Audio / cfg.Audio.Stt as "no static STT configured", so allocating it
// unconditionally would make every deployment look half-configured.
func (c *Config) applySTTEnvOverrides() {
	vals := map[string]string{
		"provider": os.Getenv("GOCLAW_AUDIO_STT_PROVIDER"),
		"api_key":  os.Getenv("GOCLAW_AUDIO_STT_API_KEY"),
		"base_url": os.Getenv("GOCLAW_AUDIO_STT_BASE_URL"),
		"model":    os.Getenv("GOCLAW_AUDIO_STT_MODEL"),
		"language": os.Getenv("GOCLAW_AUDIO_STT_LANGUAGE"),
	}
	found := false
	for _, v := range vals {
		if v != "" {
			found = true
			break
		}
	}
	if !found {
		return
	}
	if c.Audio == nil {
		c.Audio = &AudioConfig{}
	}
	if c.Audio.Stt == nil {
		c.Audio.Stt = &AudioSTTConfig{}
	}
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&c.Audio.Stt.Provider, vals["provider"])
	set(&c.Audio.Stt.APIKey, vals["api_key"])
	set(&c.Audio.Stt.BaseURL, vals["base_url"])
	set(&c.Audio.Stt.Model, vals["model"])
	set(&c.Audio.Stt.Language, vals["language"])
}

// AudioMusicConfig configures optional static Music defaults.
type AudioMusicConfig struct {
	Provider  string `json:"provider,omitempty"`  // primary provider name (e.g. "elevenlabs")
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	Fallback  string `json:"fallback,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}
