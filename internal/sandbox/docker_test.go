package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLimitedBuffer_UnderLimit(t *testing.T) {
	lb := &limitedBuffer{max: 100}
	n, err := lb.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
	if lb.truncated {
		t.Error("should not be truncated")
	}
}

func TestLimitedBuffer_AtLimit(t *testing.T) {
	lb := &limitedBuffer{max: 5}
	lb.Write([]byte("hello"))
	if lb.truncated {
		t.Error("exactly at limit should not be truncated")
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
}

func TestLimitedBuffer_OverLimit(t *testing.T) {
	lb := &limitedBuffer{max: 5}
	n, err := lb.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should report all bytes as "written" (consumed) even though truncated
	if n != 11 {
		t.Errorf("expected 11 (full input consumed), got %d", n)
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
	if !lb.truncated {
		t.Error("should be truncated")
	}
}

func TestLimitedBuffer_MultipleWrites(t *testing.T) {
	lb := &limitedBuffer{max: 10}
	lb.Write([]byte("aaaa"))
	lb.Write([]byte("bbbb"))
	lb.Write([]byte("cccc")) // should be partially truncated

	if lb.buf.Len() != 10 {
		t.Errorf("expected 10 bytes, got %d", lb.buf.Len())
	}
	if !lb.truncated {
		t.Error("should be truncated after exceeding max")
	}
	if lb.String() != "aaaabbbbcc" {
		t.Errorf("expected 'aaaabbbbcc', got %q", lb.String())
	}
}

func TestLimitedBuffer_DiscardAfterTruncation(t *testing.T) {
	lb := &limitedBuffer{max: 3}
	lb.Write([]byte("abc"))
	lb.Write([]byte("def")) // should be silently discarded

	if lb.String() != "abc" {
		t.Errorf("expected 'abc', got %q", lb.String())
	}
	if !lb.truncated {
		t.Error("should be truncated")
	}
}

func TestDefaultConfig_MaxOutputBytes(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxOutputBytes != 1<<20 {
		t.Errorf("expected 1MB default, got %d", cfg.MaxOutputBytes)
	}
}

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agent:main:telegram:direct:123", "agent-main-telegram-direct-123"},
		{"simple", "simple"},
		{"has/slash", "has-slash"},
		{"has space", "has-space"},
		{strings.Repeat("x", 100), strings.Repeat("x", 50)},
	}
	for _, tc := range tests {
		got := sanitizeKey(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeKey(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestResolveScopeKey(t *testing.T) {
	tests := []struct {
		scope    Scope
		key      string
		expected string
	}{
		{ScopeShared, "agent:main:telegram:direct:123", "shared"},
		{ScopeAgent, "agent:main:telegram:direct:123", "agent:main"},
		{ScopeSession, "agent:main:telegram:direct:123", "agent:main:telegram:direct:123"},
		{ScopeSession, "", "default"},
	}
	for _, tc := range tests {
		cfg := Config{Scope: tc.scope}
		got := cfg.ResolveScopeKey(tc.key)
		if got != tc.expected {
			t.Errorf("scope=%s key=%q → %q, want %q", tc.scope, tc.key, got, tc.expected)
		}
	}
}

func TestBuildDockerRunArgs_ReadOnlyMounts(t *testing.T) {
	workspace := t.TempDir()
	skillsStore := filepath.Join(t.TempDir(), "skills-store")
	if err := os.MkdirAll(skillsStore, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Image = "sandbox:test"
	cfg.ReadOnlyMounts = []string{skillsStore, filepath.Join(t.TempDir(), "missing"), "relative"}

	args := buildDockerRunArgs(context.Background(), "test-sandbox", cfg, workspace)
	joined := strings.Join(args, "\n")
	want := skillsStore + ":" + skillsStore + ":ro"
	if !strings.Contains(joined, want) {
		t.Fatalf("expected read-only skill mount %q in args:\n%s", want, joined)
	}
	if strings.Contains(joined, "relative:relative:ro") {
		t.Fatalf("relative read-only mount should be ignored:\n%s", joined)
	}
}
