package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Vé phiên ngắn hạn cho trình duyệt nối THẲNG tới gateway.
//
// ============================================================================
// VÌ SAO CẦN VÉ RIÊNG THAY VÌ ĐƯA API KEY XUỐNG TRÌNH DUYỆT
// ============================================================================
// Ứng dụng ngoài (Tekshot Studio, ERPcons) đã tự xác thực người dùng bằng phiên
// của chính nó. Nếu để trình duyệt cầm gateway token hay API key thì một tab bất
// kỳ đọc được nó là chạm tới toàn bộ tenant. Vé này thay thế: backend của ứng
// dụng dùng gateway token đúc một vé sống vài phút, buộc chặt vào đúng một
// người dùng và một agent, rồi trao cho trình duyệt.
//
// ============================================================================
// HAI PHẠM VI, ĐỪNG TRỘN
// ============================================================================
//   - scopePinned: khoá vào ĐÚNG một session_key. Tekshot Studio dùng — mỗi lần
//     người dùng bấm chat là một phiên phân tích dùng một lần, không có màn hình
//     lịch sử nào để đi lại.
//   - scopeUser:   cho thao tác trên MỌI phiên của chính người đó. ERPcons dùng —
//     trợ lý cá nhân phải liệt kê phiên, đọc lịch sử, đổi qua lại.
//
// scopeUser KHÔNG nới lỏng cô lập dữ liệu: vé đặt role=Operator, mà mọi handler
// của sessions.*/chat.* đều tự ép phạm vi theo client.UserID() khi role dưới
// Admin (xem methods/access.go:canSeeAll, methods/sessions.go, methods/chat.go).
// Vé chỉ quyết định ĐƯỢC GỌI METHOD NÀO, không quyết định THẤY DỮ LIỆU CỦA AI.
const (
	scopePinned = "pinned"
	scopeUser   = "user"
)

// Tool duy nhất Tekshot Studio được gọi thẳng qua tools.invoke.
const tekshotDraftPostsTool = "tekshot_generate_draft_posts"

// Trần TTL của vé. Vé sống lâu là vé bị nhặt lại dùng sau.
const maxAgentSessionTTL = 1800 * time.Second

type agentSession struct {
	TokenHash string
	UserID    string
	AgentKey  string
	// scopePinned | scopeUser — xem docblock đầu file.
	Scope string
	// Chỉ có nghĩa với scopePinned.
	PinnedSessionKey string
	// Tekshot ghi log theo trường này; scopeUser để rỗng.
	WorkspaceID string
	ExpiresAt   time.Time
	UsedAt      *time.Time
}

type agentSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]agentSession
}

func newAgentSessionStore() *agentSessionStore {
	return &agentSessionStore{sessions: make(map[string]agentSession)}
}

func (s *agentSessionStore) put(session agentSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.TokenHash] = session
}

func (s *agentSessionStore) get(rawToken string) (agentSession, bool) {
	hash := hashAgentSessionToken(rawToken)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[hash]
	if !ok || now.After(session.ExpiresAt) {
		if ok {
			delete(s.sessions, hash)
		}
		return agentSession{}, false
	}

	usedAt := now
	session.UsedAt = &usedAt
	s.sessions[hash] = session
	return session, true
}

func (s *agentSessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for hash, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, hash)
		}
	}
}

// mintOptions quyết định hình dạng của vé theo từng đường vào.
type mintOptions struct {
	scope string
	// Tiền tố ghép vào trước user id do caller gửi.
	//
	// ĐÂY LÀ ĐỊNH DANH MỘT CHIỀU: chuỗi kết quả là khoá của user_context_files,
	// sessions.user_id và toàn bộ trí nhớ của người đó. Đổi tiền tố về sau là
	// mọi người mất sạch hồ sơ. Đường /v1/tekshot/parse-sessions giữ "tekshot-"
	// vì dữ liệu đã tồn tại dưới tiền tố đó; đường dùng chung để rỗng và bắt
	// caller gửi id đầy đủ.
	userIDPrefix        string
	requirePinnedSession bool
	requireWorkspace     bool
}

type mintInput struct {
	ExternalUserID string `json:"external_user_id"`
	UserID         string `json:"user_id"`
	WorkspaceID    string `json:"workspace_id"`
	WorkspaceUUID  string `json:"workspace_uuid"`
	AgentKey       string `json:"agent_key"`
	SessionKey     string `json:"session_key"`
	TTLSeconds     int    `json:"ttl_seconds"`
}

// handleTekshotParseSession — đường CŨ của Tekshot Studio. Giữ nguyên hợp đồng:
// ghép tiền tố "tekshot-", bắt buộc workspace_id + session_key, phạm vi pinned.
func (s *Server) handleTekshotParseSession(w http.ResponseWriter, r *http.Request) {
	s.mintAgentSession(w, r, mintOptions{
		scope:                scopePinned,
		userIDPrefix:         "tekshot-",
		requirePinnedSession: true,
		requireWorkspace:     true,
	})
}

// handleAgentSession — đường DÙNG CHUNG. Caller gửi user_id đầy đủ (không ghép
// tiền tố), không cần workspace, không ghim phiên.
func (s *Server) handleAgentSession(w http.ResponseWriter, r *http.Request) {
	s.mintAgentSession(w, r, mintOptions{scope: scopeUser})
}

func (s *Server) mintAgentSession(w http.ResponseWriter, r *http.Request, opts mintOptions) {
	if r.Method != http.MethodPost {
		writeGatewayJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	// Chỉ gateway token mới đúc được vé. API key cố tình KHÔNG được chấp nhận:
	// đúc vé là hành vi của backend tin cậy, không phải của một tích hợp bất kỳ.
	if !s.hasGatewayBearer(r) {
		writeGatewayJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}

	var input mintInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "invalid JSON payload",
		})
		return
	}

	input.ExternalUserID = strings.TrimSpace(input.ExternalUserID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.WorkspaceUUID = strings.TrimSpace(input.WorkspaceUUID)
	input.AgentKey = strings.TrimSpace(input.AgentKey)
	input.SessionKey = strings.TrimSpace(input.SessionKey)

	// `user_id` là tên khoá của đường dùng chung; `external_user_id` là tên cũ
	// của Tekshot. Nhận cả hai để một client chỉ phải biết một khoá.
	rawUserID := input.UserID
	if rawUserID == "" {
		rawUserID = input.ExternalUserID
	}

	if rawUserID == "" || input.AgentKey == "" {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "user_id and agent_key are required",
		})
		return
	}
	if opts.requireWorkspace && input.WorkspaceID == "" {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "workspace_id is required",
		})
		return
	}
	if opts.requirePinnedSession && input.SessionKey == "" {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "session_key is required",
		})
		return
	}

	ttl := time.Duration(input.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 900 * time.Second
	}
	if ttl > maxAgentSessionTTL {
		ttl = maxAgentSessionTTL
	}

	rawToken, err := generateAgentSessionToken()
	if err != nil {
		slog.Error("agent_session.token_failed", "error", err)
		writeGatewayJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":      false,
			"message": "could not create agent session token",
		})
		return
	}

	userID := opts.userIDPrefix + rawUserID
	expiresAt := time.Now().Add(ttl)
	s.agentSessions.put(agentSession{
		TokenHash:        hashAgentSessionToken(rawToken),
		UserID:           userID,
		AgentKey:         input.AgentKey,
		Scope:            opts.scope,
		PinnedSessionKey: input.SessionKey,
		WorkspaceID:      input.WorkspaceID,
		ExpiresAt:        expiresAt,
	})
	s.agentSessions.cleanup()

	slog.Info("agent_session.issued",
		"scope", opts.scope,
		"workspace_id", input.WorkspaceID,
		"workspace_uuid", input.WorkspaceUUID,
		"agent_key", input.AgentKey,
		"user_id", userID,
		"session_key", input.SessionKey,
		"expires_at", expiresAt.UTC(),
	)

	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"ws_url":            s.websocketURL(r),
		"token":             rawToken,
		"user_id":           userID,
		"session_key":       input.SessionKey,
		"expires_at":        expiresAt.Unix(),
		"allowed_methods":   allowedMethodsFor(opts.scope),
		"allowed_agent_key": input.AgentKey,
	})
}

// allowedMethodsFor chỉ để client biết trước nó được gọi gì — cổng chặn thật là
// authorizeAgentSessionRPC. Hai chỗ phải khớp nhau.
func allowedMethodsFor(scope string) []string {
	if scope == scopeUser {
		return []string{
			protocol.MethodConnect,
			protocol.MethodChatSend,
			protocol.MethodChatAbort,
			protocol.MethodChatHistory,
			protocol.MethodChatInject,
			protocol.MethodChatSessionStatus,
			protocol.MethodSessionsList,
			protocol.MethodSessionsPreview,
			protocol.MethodSessionsPatch,
			protocol.MethodSessionsDelete,
			protocol.MethodSessionsReset,
		}
	}
	return []string{
		protocol.MethodConnect,
		protocol.MethodChatSend,
		protocol.MethodChatAbort,
		"tools.invoke",
	}
}

func (s *Server) hasGatewayBearer(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return false
	}
	token := strings.TrimSpace(auth[len("Bearer "):])
	return s.cfg.Gateway.Token != "" && token == s.cfg.Gateway.Token
}

// websocketURL dựng URL /ws từ chính request đúc vé.
//
// CẢNH BÁO CHO PHÍA GỌI: request này do BACKEND của ứng dụng gửi, nên Host ở đây
// là địa chỉ mà BACKEND dùng để tới gateway — có thể là tên container hoặc IP nội
// bộ mà trình duyệt không bao giờ tới được. Chạy sau reverse proxy thì đặt
// X-Forwarded-Host/-Proto; không thì phía gọi phải tự ghi đè giá trị này trước
// khi trao cho trình duyệt. Sai chỗ này hỏng IM LẶNG: vé hợp lệ, WebSocket chỉ
// đơn giản là không nối được.
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

func generateAgentSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "goclaw_ephemeral_" + hex.EncodeToString(buf), nil
}

func hashAgentSessionToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func writeGatewayJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (c *Client) setAgentSession(session agentSession) {
	c.agentSession = &session
	// Operator, KHÔNG phải Admin — đây là thứ khiến canSeeAll() trả false và mọi
	// handler sessions.*/chat.* tự lọc theo client.UserID(). Nới lên Admin là mở
	// toang dữ liệu của cả tenant cho một vé trình duyệt.
	c.role = permissions.RoleOperator
	c.authenticated = true
	c.userID = session.UserID
	c.tenantID = store.MasterTenantID
}

// authorizeAgentSessionRPC là cổng chặn method cho client cầm vé.
//
// Nó KHÔNG lo phạm vi dữ liệu — việc đó từng handler tự làm theo client.UserID().
// Ở đây chỉ trả lời một câu: vé này có được gọi method này không.
func (c *Client) authorizeAgentSessionRPC(req *protocol.RequestFrame) bool {
	session := c.agentSession
	if session == nil {
		return true
	}

	if time.Now().After(session.ExpiresAt) {
		return false
	}

	if session.Scope == scopeUser {
		return authorizeUserScopedRPC(session, req)
	}
	return authorizePinnedRPC(session, req)
}

func authorizeUserScopedRPC(session *agentSession, req *protocol.RequestFrame) bool {
	switch req.Method {
	case protocol.MethodChatSend:
		// Không so khớp sessionKey: người dùng phải đổi phiên được, và quyền trên
		// từng phiên do handleSend tự kiểm. Nhưng agent thì phải đúng agent của vé.
		var params struct {
			AgentID string `json:"agentId"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.AgentID == session.AgentKey

	case protocol.MethodChatAbort,
		protocol.MethodChatHistory,
		protocol.MethodChatInject,
		protocol.MethodChatSessionStatus,
		protocol.MethodSessionsList,
		protocol.MethodSessionsPreview,
		protocol.MethodSessionsPatch,
		protocol.MethodSessionsDelete,
		protocol.MethodSessionsReset:
		return true
	}

	// tools.invoke CỐ Ý không có mặt: trợ lý cá nhân không cần trình duyệt gọi
	// thẳng tool nào. Tool mà agent tự dùng trong một lượt chat đi qua agent
	// loop, không qua router này, nên không bị chặn ở đây.
	return false
}

func authorizePinnedRPC(session *agentSession, req *protocol.RequestFrame) bool {
	switch req.Method {
	case protocol.MethodChatSend:
		var params struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.AgentID == session.AgentKey && params.SessionKey == session.PinnedSessionKey

	case protocol.MethodChatAbort:
		var params struct {
			SessionKey string `json:"sessionKey"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.SessionKey == session.PinnedSessionKey

	case "tools.invoke":
		var params struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
			Tool       string `json:"tool"`
		}
		if req.Params == nil || json.Unmarshal(req.Params, &params) != nil {
			return false
		}
		return params.AgentID == session.AgentKey &&
			params.SessionKey == session.PinnedSessionKey &&
			params.Tool == tekshotDraftPostsTool
	}

	return false
}
