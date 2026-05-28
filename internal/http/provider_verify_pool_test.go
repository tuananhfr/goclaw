package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type verifyRouteTokenSource struct {
	token       string
	eligibility providers.RouteEligibility
}

func (s verifyRouteTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s verifyRouteTokenSource) RouteEligibility(context.Context) providers.RouteEligibility {
	return s.eligibility
}

func writeVerifySSEDone(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp-verify","status":"completed"}}`+"\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestVerifyProviderUsesCodexPoolFallback(t *testing.T) {
	tenantID := uuid.New()

	var primaryCalls atomic.Int32
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "primary should be skipped", http.StatusTooManyRequests)
	}))
	defer primaryServer.Close()

	var backupCalls atomic.Int32
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		writeVerifySSEDone(w)
	}))
	defer backupServer.Close()

	providerStore := newMockProviderStore()
	primary := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		APIKey:       "primary-token",
		APIBase:      primaryServer.URL,
		Enabled:      true,
	}
	backup := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex-backup",
		ProviderType: store.ProviderChatGPTOAuth,
		APIKey:       "backup-token",
		APIBase:      backupServer.URL,
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(context.Background(), primary); err != nil {
		t.Fatalf("CreateProvider(primary) error = %v", err)
	}
	if err := providerStore.CreateProvider(context.Background(), backup); err != nil {
		t.Fatalf("CreateProvider(backup) error = %v", err)
	}

	providerReg := providers.NewRegistry(nil)
	providerReg.RegisterForTenant(tenantID, providers.NewCodexProvider(
		primary.Name,
		verifyRouteTokenSource{
			token: "primary-token",
			eligibility: providers.RouteEligibility{
				Class:  providers.RouteEligibilityBlocked,
				Reason: "exhausted",
			},
		},
		primaryServer.URL,
		"gpt-5.4",
	).WithRoutingDefaults(store.ChatGPTOAuthStrategyPriority, []string{backup.Name}))
	providerReg.RegisterForTenant(tenantID, providers.NewCodexProvider(
		backup.Name,
		verifyRouteTokenSource{
			token:       "backup-token",
			eligibility: providers.RouteEligibility{Class: providers.RouteEligibilityHealthy},
		},
		backupServer.URL,
		"gpt-5.4",
	))

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), providerReg, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+primary.ID.String()+"/verify", bytes.NewBufferString(`{"model":"gpt-5.4"}`))
	req.SetPathValue("id", primary.ID.String())
	w := httptest.NewRecorder()

	handler.HandleVerifyProviderForTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	var body struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if !body.Valid {
		t.Fatalf("valid = false, error = %q", body.Error)
	}
	if got := primaryCalls.Load(); got != 0 {
		t.Fatalf("primary calls = %d, want 0 blocked primary to be skipped", got)
	}
	if got := backupCalls.Load(); got != 1 {
		t.Fatalf("backup calls = %d, want 1", got)
	}
}
