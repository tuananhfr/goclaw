package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

func TestVaultDocumentScopeDenyByDefault(t *testing.T) {
	id := uuid.NewString()
	if !vaultDocumentAllowed(context.Background(), id) {
		t.Fatal("ordinary requests must retain existing access")
	}
	if vaultDocumentAllowed(WithVaultDocumentScope(context.Background(), nil), id) {
		t.Fatal("explicit empty public scope must deny every document")
	}
	ids := []string{id}
	ctx := WithVaultDocumentScope(context.Background(), ids)
	ids[0] = uuid.NewString()
	if !vaultDocumentAllowed(ctx, id) || vaultDocumentAllowed(ctx, ids[0]) {
		t.Fatal("request scope must own an immutable copy")
	}
}

func TestVaultDocumentScopeSearchExcludesPrivateSources(t *testing.T) {
	id := uuid.NewString()
	ctx := WithVaultDocumentScope(context.Background(), []string{id})
	results := restrictVaultResults(ctx, []vault.UnifiedSearchResult{
		{ID: id, Source: "vault"},
		{ID: uuid.NewString(), Source: "vault", Snippet: "private"},
		{ID: id, Source: "episodic", Snippet: "customer memory"},
		{ID: id, Source: "kg", Snippet: "private graph"},
	})
	if len(results) != 1 || results[0].Source != "vault" {
		t.Fatalf("public search leaked sources: %+v", results)
	}
}

func TestVaultDocumentScopeBlocksReadAndLinkedTitles(t *testing.T) {
	tenant, agent, approved, private := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	agentID := agent.String()
	doc := &store.VaultDocument{ID: approved.String(), TenantID: tenant.String(), AgentID: &agentID, Scope: "personal", Path: "approved.md", DocType: "context"}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, doc.Path, "Public information")
	ctx := WithVaultDocumentScope(makeCtx(tenant, agent), []string{approved.String()})
	denied := tool.Execute(ctx, map[string]any{"doc_id": private.String()})
	if !denied.IsError || !strings.Contains(denied.ForLLM, "scope") {
		t.Fatalf("unapproved reads must be rejected before lookup: %+v", denied)
	}
	if tool.allowed(ctx, &store.VaultDocument{ID: private.String(), TenantID: tenant.String(), AgentID: &agentID, Scope: "personal"}) {
		t.Fatal("unapproved linked document metadata must not be exposed")
	}
}
