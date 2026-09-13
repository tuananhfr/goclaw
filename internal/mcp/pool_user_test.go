package mcp

import (
	"testing"
	"time"
)

// newIdleUserEntry registers a fake user connection whose lastUsed is already
// past UserIdleTTL. No real client: evictIdle only closes a non-nil clientPtr,
// and cancel is nil, so the eviction path runs end to end without a server.
func newIdleUserEntry(p *Pool, key string, age time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.userServers[key] = &poolEntry{
		state:    &serverState{name: "erpcons"},
		refCount: 0,
		lastUsed: time.Now().Add(-age),
	}
}

func hasUserEntry(p *Pool, key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.userServers[key]
	return ok
}

// A user connection with refCount 0 (the normal state — getUserMCPTools releases
// straight after acquiring) must survive eviction as long as something keeps
// touching it. Before TouchUser existed, this connection was dropped every
// UserIdleTTL no matter how actively the person was chatting.
func TestPool_TouchUser_KeepsActiveConnection(t *testing.T) {
	p := NewPool(PoolConfig{MaxSize: 5, UserIdleTTL: time.Minute})
	t.Cleanup(func() { p.Stop() })

	const key = "tenant/erpcons/user:erpcons-28"
	newIdleUserEntry(p, key, 90*time.Second)

	p.TouchUser(key)
	p.evictIdle()
	if !hasUserEntry(p, key) {
		t.Fatal("touched connection was evicted")
	}

	// Untouched, the same entry ages out.
	newIdleUserEntry(p, key, 90*time.Second)
	p.evictIdle()
	if hasUserEntry(p, key) {
		t.Fatal("idle connection survived eviction")
	}
}

func TestPool_TouchUser_UnknownKey(t *testing.T) {
	p := NewPool(PoolConfig{MaxSize: 5})
	t.Cleanup(func() { p.Stop() })
	// Callers touch every user-credential server; a user may have credentials
	// for only some of them, so unknown keys must be a no-op, not a panic.
	p.TouchUser("tenant/erpcons/user:nobody")
}

func TestDefaultPoolConfig_EnvOverride(t *testing.T) {
	t.Setenv("GOCLAW_MCP_MAX_USER_CONNS", "500")
	t.Setenv("GOCLAW_MCP_USER_IDLE_TTL", "3m")

	cfg := DefaultPoolConfig()
	if cfg.MaxUserConns != 500 {
		t.Errorf("MaxUserConns: got %d, want 500", cfg.MaxUserConns)
	}
	if cfg.UserIdleTTL != 3*time.Minute {
		t.Errorf("UserIdleTTL: got %s, want 3m", cfg.UserIdleTTL)
	}
}

// A typo must not shrink the pool to zero or disable eviction.
func TestDefaultPoolConfig_EnvGarbageFallsBack(t *testing.T) {
	t.Setenv("GOCLAW_MCP_MAX_USER_CONNS", "many")
	t.Setenv("GOCLAW_MCP_USER_IDLE_TTL", "-5m")

	cfg := DefaultPoolConfig()
	if cfg.MaxUserConns != 100 {
		t.Errorf("MaxUserConns: got %d, want default 100", cfg.MaxUserConns)
	}
	if cfg.UserIdleTTL != 15*time.Minute {
		t.Errorf("UserIdleTTL: got %s, want default 15m", cfg.UserIdleTTL)
	}
}

func TestDefaultPoolConfig_NoEnv(t *testing.T) {
	t.Setenv("GOCLAW_MCP_MAX_USER_CONNS", "")
	t.Setenv("GOCLAW_MCP_USER_IDLE_TTL", "")

	cfg := DefaultPoolConfig()
	if cfg.MaxUserConns != 100 || cfg.UserIdleTTL != 15*time.Minute {
		t.Errorf("defaults: got %d / %s", cfg.MaxUserConns, cfg.UserIdleTTL)
	}
}
