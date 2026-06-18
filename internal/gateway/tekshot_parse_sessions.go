package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type tekshotParseSession struct {
	TokenHash   string
	UserID      string
	AgentKey    string
	SessionKey  string
	WorkspaceID string
	ExpiresAt   time.Time
	UsedAt      *time.Time
}

type tekshotParseSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]tekshotParseSession
}

func newTekshotParseSessionStore() *tekshotParseSessionStore {
	return &tekshotParseSessionStore{sessions: make(map[string]tekshotParseSession)}
}

func (s *tekshotParseSessionStore) put(session tekshotParseSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.TokenHash] = session
}

func (s *tekshotParseSessionStore) get(rawToken string) (tekshotParseSession, bool) {
	hash := hashTekshotToken(rawToken)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[hash]
	if !ok || now.After(session.ExpiresAt) {
		if ok {
			delete(s.sessions, hash)
		}
		return tekshotParseSession{}, false
	}

	usedAt := now
	session.UsedAt = &usedAt
	s.sessions[hash] = session
	return session, true
}

func (s *tekshotParseSessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for hash, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, hash)
		}
	}
}

func (s *Server) handleTekshotParseSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTekshotJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	if !s.hasGatewayBearer(r) {
		writeTekshotJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}

	var input struct {
		ExternalUserID string `json:"external_user_id"`
		WorkspaceID    string `json:"workspace_id"`
		WorkspaceUUID  string `json:"workspace_uuid"`
		AgentKey       string `json:"agent_key"`
		SessionKey     string `json:"session_key"`
		TTLSeconds     int    `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "invalid JSON payload",
		})
		return
	}

	input.ExternalUserID = strings.TrimSpace(input.ExternalUserID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.WorkspaceUUID = strings.TrimSpace(input.WorkspaceUUID)
	input.AgentKey = strings.TrimSpace(input.AgentKey)
	input.SessionKey = strings.TrimSpace(input.SessionKey)

	if input.ExternalUserID == "" || input.WorkspaceID == "" || input.AgentKey == "" || input.SessionKey == "" {
		writeTekshotJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "external_user_id, workspace_id, agent_key, and session_key are required",
		})
		return
	}

	ttl := input.TTLSeconds
	if ttl <= 0 {
		ttl = 900
	}
	if ttl > 1800 {
		ttl = 1800
	}

	rawToken, err := generateTekshotToken()
	if err != nil {
		slog.Error("tekshot.parse_session.token_failed", "error", err)
		writeTekshotJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":      false,
			"message": "could not create parse session token",
		})
		return
	}

	userID := "tekshot-" + input.ExternalUserID
	expiresAt := time.Now().Add(time.Duration(ttl) * time.Second)
	s.tekshotParseSessions.put(tekshotParseSession{
		TokenHash:   hashTekshotToken(rawToken),
		UserID:      userID,
		AgentKey:    input.AgentKey,
		SessionKey:  input.SessionKey,
		WorkspaceID: input.WorkspaceID,
		ExpiresAt:   expiresAt,
	})
	s.tekshotParseSessions.cleanup()

	slog.Info("tekshot.parse_session.issued",
		"workspace_id", input.WorkspaceID,
		"workspace_uuid", input.WorkspaceUUID,
		"agent_key", input.AgentKey,
		"user_id", userID,
		"session_key", input.SessionKey,
		"expires_at", expiresAt.UTC(),
	)

	writeTekshotJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"ws_url":            s.websocketURL(r),
		"token":             rawToken,
		"user_id":           userID,
		"session_key":       input.SessionKey,
		"expires_at":        expiresAt.Unix(),
		"allowed_methods":   []string{protocol.MethodConnect, protocol.MethodChatSend, protocol.MethodChatAbort, "tools.invoke"},
		"allowed_agent_key": input.AgentKey,
	})
}

func (s *Server) handleTekshotGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTekshotJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	targetRaw := strings.TrimSpace(os.Getenv("TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET"))
	if targetRaw == "" {
		writeTekshotJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET is not configured",
		})
		return
	}

	target, err := url.Parse(targetRaw)
	if err != nil || target.Scheme == "" || target.Host == "" {
		writeTekshotJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET must be an absolute URL",
		})
		return
	}

	if !isAllowedTekshotOAuthCallbackTarget(target) {
		writeTekshotJSON(w, http.StatusForbidden, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET is not allowed",
		})
		return
	}

	target.RawQuery = r.URL.RawQuery
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func isAllowedTekshotOAuthCallbackTarget(target *url.URL) bool {
	host := strings.ToLower(target.Hostname())
	if target.Scheme == "https" {
		return true
	}
	if target.Scheme != "http" {
		return false
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "tekshot.localhost"
}

func (s *Server) hasGatewayBearer(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return false
	}
	token := strings.TrimSpace(auth[len("Bearer "):])
	return s.cfg.Gateway.Token != "" && token == s.cfg.Gateway.Token
}

func (s *Server) websocketURL(r *http.Request) string {
	scheme := "wss"
	if r.TLS == nil {
		scheme = "ws"
	}
	if proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto == "https" {
		scheme = "wss"
	} else if proto == "http" {
		scheme = "ws"
	}
	host := r.Host
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + "/ws"
}

func generateTekshotToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "goclaw_ephemeral_" + hex.EncodeToString(buf), nil
}

func hashTekshotToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func writeTekshotJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (c *Client) setTekshotParseSession(session tekshotParseSession) {
	c.tekshotParseSession = &session
	c.role = permissions.RoleOperator
	c.authenticated = true
	c.userID = session.UserID
	c.tenantID = store.MasterTenantID
}

func (c *Client) authorizeTekshotRPC(req *protocol.RequestFrame) bool {
	session := c.tekshotParseSession
	if session == nil {
		return true
	}

	if time.Now().After(session.ExpiresAt) {
		return false
	}

	switch req.Method {
	case protocol.MethodChatSend:
		var params struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.AgentID == session.AgentKey && params.SessionKey == session.SessionKey

	case protocol.MethodChatAbort:
		var params struct {
			SessionKey string `json:"sessionKey"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.SessionKey == session.SessionKey

	case "tools.invoke":
		var params struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
			Tool       string `json:"tool"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.AgentID == session.AgentKey && params.SessionKey == session.SessionKey && params.Tool == "tekshot_generate_draft_posts"
	}

	return false
}
