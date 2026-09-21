package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTekshotGovernance_RequiresGatewayToken(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.Token = "secret"

	req := httptest.NewRequest(http.MethodGet, "/v1/tekshot/governance", nil)
	w := httptest.NewRecorder()
	s.handleTekshotGovernance(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHandleTekshotGovernance_RejectsNonGet(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.Token = "secret"

	req := httptest.NewRequest(http.MethodPost, "/v1/tekshot/governance", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.handleTekshotGovernance(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandleTekshotGovernance_ReturnsCatalog(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.Token = "secret"

	req := httptest.NewRequest(http.MethodGet, "/v1/tekshot/governance", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.handleTekshotGovernance(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		OK         bool `json:"ok"`
		Governance struct {
			Version       string           `json:"version"`
			AbsoluteRules []map[string]any `json:"absolute_rules"`
		} `json:"governance"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Governance.Version == "" || len(body.Governance.AbsoluteRules) != 15 {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}
