package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestProductVisualProfileToolUpsertGetListDelete(t *testing.T) {
	tool := NewProductVisualProfileTool(t.TempDir())
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	upsert := tool.Execute(ctx, map[string]any{
		"action":                 "upsert",
		"product_key":            "banh-dua",
		"product_name":           "Banh dua",
		"mcp_server_id":          "bakery-drive",
		"drive_folder_id":        "folder-1",
		"visual_summary":         "Round coconut cakes on a white tray.",
		"packaging_summary":      "Clear box with green label.",
		"best_reference_file_id": "file-1",
		"asset_versions": map[string]any{
			"file-1": "2026-05-24T00:00:00Z:123",
		},
	})
	if upsert.IsError {
		t.Fatalf("upsert failed: %s", upsert.ForLLM)
	}

	get := tool.Execute(ctx, map[string]any{
		"action":          "get",
		"product_key":     "banh-dua",
		"mcp_server_id":   "bakery-drive",
		"drive_folder_id": "folder-1",
	})
	if get.IsError {
		t.Fatalf("get failed: %s", get.ForLLM)
	}
	var profile productVisualProfile
	if err := json.Unmarshal([]byte(get.ForLLM), &profile); err != nil {
		t.Fatalf("unmarshal get result: %v", err)
	}
	if profile.ProductKey != "banh-dua" || profile.AssetVersions["file-1"] == "" {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	list := tool.Execute(ctx, map[string]any{
		"action":        "list",
		"mcp_server_id": "bakery-drive",
	})
	if list.IsError || !strings.Contains(list.ForLLM, `"count": 1`) {
		t.Fatalf("unexpected list result: error=%v output=%s", list.IsError, list.ForLLM)
	}

	del := tool.Execute(ctx, map[string]any{
		"action":          "delete",
		"product_key":     "banh-dua",
		"mcp_server_id":   "bakery-drive",
		"drive_folder_id": "folder-1",
	})
	if del.IsError {
		t.Fatalf("delete failed: %s", del.ForLLM)
	}

	missing := tool.Execute(ctx, map[string]any{
		"action":          "get",
		"product_key":     "banh-dua",
		"mcp_server_id":   "bakery-drive",
		"drive_folder_id": "folder-1",
	})
	if missing.IsError || !strings.Contains(missing.ForLLM, `"found":false`) {
		t.Fatalf("unexpected missing result: error=%v output=%s", missing.IsError, missing.ForLLM)
	}
}
