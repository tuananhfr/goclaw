package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeVaultContentStore struct {
	store.VaultStore
	doc          *store.VaultDocument
	updatedDoc   *store.VaultDocument
	updateCalled bool
}

func (f *fakeVaultContentStore) GetDocumentByID(ctx context.Context, tenantID, id string) (*store.VaultDocument, error) {
	if f.doc == nil || f.doc.TenantID != tenantID || f.doc.ID != id {
		return nil, os.ErrNotExist
	}
	return f.doc, nil
}

func (f *fakeVaultContentStore) UpdateDocumentAfterContentWrite(ctx context.Context, tenantID, docID, title, docType string, metadata map[string]any, contentHash string, contentExcerpt string) (*store.VaultDocument, error) {
	f.updateCalled = true
	doc := *f.doc
	doc.Title = title
	doc.DocType = docType
	doc.Metadata = metadata
	doc.ContentHash = contentHash
	doc.ContentExcerpt = contentExcerpt
	doc.Summary = ""
	doc.UpdatedAt = time.Now().UTC()
	f.updatedDoc = &doc
	return f.updatedDoc, nil
}

type fakeVaultContentBus struct {
	events []eventbus.DomainEvent
}

func (f *fakeVaultContentBus) Publish(event eventbus.DomainEvent) {
	f.events = append(f.events, event)
}

func (f *fakeVaultContentBus) Subscribe(eventbus.EventType, eventbus.DomainEventHandler) func() {
	return func() {}
}

func (f *fakeVaultContentBus) Start(context.Context) {}

func (f *fakeVaultContentBus) Drain(time.Duration) error { return nil }

func vaultContentTestContext(tenantID uuid.UUID) context.Context {
	return store.WithRole(store.WithTenantID(context.Background(), tenantID), store.RoleOwner)
}

func TestVaultHandlerGetDocumentContent(t *testing.T) {
	tenantID := uuid.New()
	docID := uuid.New().String()
	workspaceRoot := t.TempDir()
	ws := config.TenantWorkspace(workspaceRoot, tenantID, "")
	relPath := "agents/page/menu.md"
	if err := os.MkdirAll(filepath.Join(ws, "agents", "page"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, filepath.FromSlash(relPath)), []byte("# Menu\nCoffee"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := &VaultHandler{
		store: &fakeVaultContentStore{doc: &store.VaultDocument{
			ID: docID, TenantID: tenantID.String(), Path: relPath,
			Title: "Menu", DocType: "context", Scope: "shared",
		}},
		workspace: workspaceRoot,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/vault/documents/"+docID+"/content", nil)
	req = req.WithContext(vaultContentTestContext(tenantID))
	req.SetPathValue("docID", docID)
	rr := httptest.NewRecorder()

	h.handleGetDocumentContent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got vaultDocumentContentResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Content != "# Menu\nCoffee" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.Document == nil || got.Document.ID != docID {
		t.Fatalf("document missing: %+v", got.Document)
	}
}

func TestVaultHandlerUpdateDocumentContent(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New().String()
	docID := uuid.New().String()
	workspaceRoot := t.TempDir()
	ws := config.TenantWorkspace(workspaceRoot, tenantID, "")
	relPath := "agents/page/menu.md"
	fullPath := filepath.Join(ws, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fakeStore := &fakeVaultContentStore{doc: &store.VaultDocument{
		ID: docID, TenantID: tenantID.String(), AgentID: &agentID, Path: relPath,
		Title: "Old Menu", DocType: "context", Scope: "personal", Summary: "stale summary",
	}}
	fakeBus := &fakeVaultContentBus{}
	h := &VaultHandler{store: fakeStore, workspace: workspaceRoot, eventBus: fakeBus}

	body := []byte(`{"title":"New Menu","content":"# Menu\nTea","doc_type":"context","metadata":{"source":"test"}}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/"+agentID+"/vault/documents/"+docID+"/content", bytes.NewReader(body))
	req = req.WithContext(vaultContentTestContext(tenantID))
	req.SetPathValue("agentID", agentID)
	req.SetPathValue("docID", docID)
	rr := httptest.NewRecorder()

	h.handleUpdateDocumentContent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	written, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read written: %v", err)
	}
	if string(written) != "# Menu\nTea" {
		t.Fatalf("written content = %q", string(written))
	}
	if !fakeStore.updateCalled {
		t.Fatalf("store update was not called")
	}
	if fakeStore.updatedDoc == nil || fakeStore.updatedDoc.Summary != "" {
		t.Fatalf("summary was not cleared: %+v", fakeStore.updatedDoc)
	}
	if len(fakeBus.events) != 1 {
		t.Fatalf("events = %d, want 1", len(fakeBus.events))
	}
	payload, ok := fakeBus.events[0].Payload.(eventbus.VaultDocUpsertedPayload)
	if !ok {
		t.Fatalf("payload type = %T", fakeBus.events[0].Payload)
	}
	if payload.DocID != docID || payload.ContentHash == "" || payload.Workspace != ws {
		t.Fatalf("bad payload: %+v", payload)
	}
}
