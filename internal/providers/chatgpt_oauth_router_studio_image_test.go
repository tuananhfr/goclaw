package providers

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestChatGPTOAuthRouterStudioImage_FirstRetryable_SecondSucceeds(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)
	serverA := retryableImageTestServer(t)
	serverB := imageTestServer(t, func() string { return okStudioStream })
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))
	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "priority_order", []string{"acct-b"})

	res, err := router.StudioImage(context.Background(), StudioImageRequest{Text: "x"})
	if err != nil {
		t.Fatalf("StudioImage: %v", err)
	}
	if len(res.Data) == 0 || res.Text == "" {
		t.Fatalf("expected image + text from member B, got %+v", res)
	}
}

func TestChatGPTOAuthRouterStudioImage_NonRetryable_ReturnsImmediately(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)
	hitsB := 0
	serverA := badRequestImageTestServer(t)
	serverB := imageTestServer(t, func() string { hitsB++; return okStudioStream })
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))
	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "priority_order", []string{"acct-b"})

	if _, err := router.StudioImage(context.Background(), StudioImageRequest{Text: "x"}); err == nil {
		t.Fatal("want error from non-retryable 400")
	}
	if hitsB != 0 {
		t.Fatalf("member B hit %d times, want 0", hitsB)
	}
}
