package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func frame(method string, params map[string]any) *protocol.RequestFrame {
	f := &protocol.RequestFrame{Type: "req", ID: "1", Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			panic(err)
		}
		f.Params = raw
	}
	return f
}

func clientWith(session agentSession) *Client {
	return &Client{agentSession: &session}
}

func liveSession(scope string) agentSession {
	return agentSession{
		UserID:           "erpcons-42",
		AgentKey:         "erpcons-assistant",
		Scope:            scope,
		PinnedSessionKey: "tekshot:workspace:1:chat:abc",
		ExpiresAt:        time.Now().Add(10 * time.Minute),
	}
}

// Vé hết hạn phải chặn MỌI method, kể cả method nằm trong allowlist.
func TestAgentSession_Expired_DeniesEverything(t *testing.T) {
	s := liveSession(scopeUser)
	s.ExpiresAt = time.Now().Add(-time.Second)
	c := clientWith(s)

	for _, m := range []string{protocol.MethodChatSend, protocol.MethodSessionsList, protocol.MethodChatHistory} {
		if c.authorizeAgentSessionRPC(frame(m, map[string]any{"agentId": "erpcons-assistant"})) {
			t.Fatalf("expired ticket must deny %s", m)
		}
	}
}

// Client không cầm vé thì đi tiếp bằng đường phân quyền thường (policy engine).
func TestAgentSession_NoTicket_FallsThrough(t *testing.T) {
	c := &Client{}
	if !c.authorizeAgentSessionRPC(frame(protocol.MethodConfigApply, nil)) {
		t.Fatal("client without a ticket must fall through to the policy engine")
	}
}

// ---------------------------------------------------------------------------
// scopePinned — hành vi CŨ của Tekshot Studio, không được đổi.
// ---------------------------------------------------------------------------

func TestPinnedScope_KeepsTekshotContract(t *testing.T) {
	c := clientWith(liveSession(scopePinned))

	cases := []struct {
		name   string
		frame  *protocol.RequestFrame
		expect bool
	}{
		{"chat.send đúng agent + đúng phiên ghim", frame(protocol.MethodChatSend, map[string]any{
			"agentId": "erpcons-assistant", "sessionKey": "tekshot:workspace:1:chat:abc",
		}), true},
		{"chat.send sai phiên", frame(protocol.MethodChatSend, map[string]any{
			"agentId": "erpcons-assistant", "sessionKey": "tekshot:workspace:1:chat:CUA-NGUOI-KHAC",
		}), false},
		{"chat.send sai agent", frame(protocol.MethodChatSend, map[string]any{
			"agentId": "agent-khac", "sessionKey": "tekshot:workspace:1:chat:abc",
		}), false},
		{"chat.abort đúng phiên", frame(protocol.MethodChatAbort, map[string]any{
			"sessionKey": "tekshot:workspace:1:chat:abc",
		}), true},
		{"tools.invoke đúng tool", frame("tools.invoke", map[string]any{
			"agentId": "erpcons-assistant", "sessionKey": "tekshot:workspace:1:chat:abc",
			"tool": tekshotDraftPostsTool,
		}), true},
		{"tools.invoke tool khác", frame("tools.invoke", map[string]any{
			"agentId": "erpcons-assistant", "sessionKey": "tekshot:workspace:1:chat:abc",
			"tool": "exec",
		}), false},
		// Vé pinned KHÔNG được mở rộng theo scopeUser — đây là chốt hồi quy.
		{"chat.history bị chặn", frame(protocol.MethodChatHistory, map[string]any{
			"sessionKey": "tekshot:workspace:1:chat:abc",
		}), false},
		{"sessions.list bị chặn", frame(protocol.MethodSessionsList, nil), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.authorizeAgentSessionRPC(tc.frame); got != tc.expect {
				t.Fatalf("got %v, want %v", got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// scopeUser — trợ lý cá nhân ERPcons.
// ---------------------------------------------------------------------------

func TestUserScope_AllowsFullChatSurface(t *testing.T) {
	c := clientWith(liveSession(scopeUser))

	allowed := []*protocol.RequestFrame{
		frame(protocol.MethodChatSend, map[string]any{"agentId": "erpcons-assistant"}),
		// Phiên bất kỳ, không ghim: người dùng phải đổi phiên được.
		frame(protocol.MethodChatSend, map[string]any{
			"agentId": "erpcons-assistant", "sessionKey": "ws:erpcons-assistant:phien-khac",
		}),
		frame(protocol.MethodChatAbort, map[string]any{"sessionKey": "bat-ky"}),
		frame(protocol.MethodChatHistory, map[string]any{"sessionKey": "bat-ky"}),
		frame(protocol.MethodChatInject, map[string]any{"sessionKey": "bat-ky"}),
		frame(protocol.MethodChatSessionStatus, map[string]any{"sessionKey": "bat-ky"}),
		frame(protocol.MethodSessionsList, nil),
		frame(protocol.MethodSessionsPreview, map[string]any{"key": "bat-ky"}),
		frame(protocol.MethodSessionsPatch, map[string]any{"key": "bat-ky"}),
		frame(protocol.MethodSessionsDelete, map[string]any{"key": "bat-ky"}),
		frame(protocol.MethodSessionsReset, map[string]any{"key": "bat-ky"}),
	}

	for _, f := range allowed {
		if !c.authorizeAgentSessionRPC(f) {
			t.Fatalf("scopeUser must allow %s", f.Method)
		}
	}
}

func TestUserScope_DeniesEverythingElse(t *testing.T) {
	c := clientWith(liveSession(scopeUser))

	denied := []*protocol.RequestFrame{
		// Agent khác — vé chỉ mở đúng một agent.
		frame(protocol.MethodChatSend, map[string]any{"agentId": "agent-khac"}),
		// Thiếu agentId.
		frame(protocol.MethodChatSend, map[string]any{"sessionKey": "x"}),
		// tools.invoke CỐ Ý không có trong scopeUser.
		frame("tools.invoke", map[string]any{
			"agentId": "erpcons-assistant", "tool": tekshotDraftPostsTool,
		}),
		// Mặt phẳng quản trị.
		frame(protocol.MethodConfigApply, nil),
		frame(protocol.MethodAgentsCreate, nil),
		frame(protocol.MethodAgentsDelete, map[string]any{"id": "x"}),
		frame(protocol.MethodSessionsCompact, map[string]any{"key": "x"}),
	}

	for _, f := range denied {
		if c.authorizeAgentSessionRPC(f) {
			t.Fatalf("scopeUser must deny %s", f.Method)
		}
	}
}

// allowedMethods trả cho client phải KHỚP cổng chặn thật. Lệch nhau là client
// hiển thị được nút mà bấm vào thì gateway từ chối.
func TestAllowedMethodsMatchesAuthorizer(t *testing.T) {
	for _, scope := range []string{scopePinned, scopeUser} {
		c := clientWith(liveSession(scope))
		for _, m := range allowedMethodsFor(scope) {
			if m == protocol.MethodConnect {
				continue // connect xử lý trước khi vé được gắn
			}
			params := map[string]any{
				"agentId":    "erpcons-assistant",
				"sessionKey": "tekshot:workspace:1:chat:abc",
				"key":        "tekshot:workspace:1:chat:abc",
				"tool":       tekshotDraftPostsTool,
			}
			if !c.authorizeAgentSessionRPC(frame(m, params)) {
				t.Fatalf("scope %q quảng cáo %q nhưng authorizer từ chối", scope, m)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Kho vé
// ---------------------------------------------------------------------------

func TestAgentSessionStore_ExpiredTicketIsDropped(t *testing.T) {
	store := newAgentSessionStore()
	raw := "goclaw_ephemeral_test"
	store.put(agentSession{
		TokenHash: hashAgentSessionToken(raw),
		UserID:    "erpcons-42",
		Scope:     scopeUser,
		ExpiresAt: time.Now().Add(-time.Second),
	})

	if _, ok := store.get(raw); ok {
		t.Fatal("expired ticket must not resolve")
	}
	if len(store.sessions) != 0 {
		t.Fatal("expired ticket must be evicted on lookup")
	}
}

func TestAgentSessionStore_RawTokenIsNotStored(t *testing.T) {
	store := newAgentSessionStore()
	raw, err := generateAgentSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	store.put(agentSession{
		TokenHash: hashAgentSessionToken(raw),
		ExpiresAt: time.Now().Add(time.Minute),
	})

	for hash, s := range store.sessions {
		if hash == raw || s.TokenHash == raw {
			t.Fatal("raw token must never be persisted, only its SHA-256 hash")
		}
	}
	if _, ok := store.get(raw); !ok {
		t.Fatal("valid ticket must resolve by raw token")
	}
}
