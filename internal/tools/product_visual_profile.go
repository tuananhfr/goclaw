package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ProductVisualProfileTool stores durable visual summaries for product image folders.
type ProductVisualProfileTool struct {
	dataDir string
}

type productVisualProfile struct {
	ProductKey          string            `json:"product_key"`
	ProductName         string            `json:"product_name,omitempty"`
	MCPServerID         string            `json:"mcp_server_id"`
	DriveFolderID       string            `json:"drive_folder_id"`
	VisualSummary       string            `json:"visual_summary,omitempty"`
	PackagingSummary    string            `json:"packaging_summary,omitempty"`
	BestReferenceFileID string            `json:"best_reference_file_id,omitempty"`
	AssetVersions       map[string]string `json:"asset_versions,omitempty"`
	UpdatedAt           string            `json:"updated_at"`
}

func NewProductVisualProfileTool(dataDir string) *ProductVisualProfileTool {
	return &ProductVisualProfileTool{dataDir: dataDir}
}

func (t *ProductVisualProfileTool) Name() string { return "product_visual_profile" }

func (t *ProductVisualProfileTool) Description() string {
	return "Get, list, upsert, or delete durable product visual profiles for Google Drive product folders. Use after gdrive_get_product_assets when a product is new or Drive assets changed."
}

func (t *ProductVisualProfileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "list", "upsert", "delete"},
				"description": "Operation to perform.",
			},
			"product_key": map[string]any{
				"type":        "string",
				"description": "Stable slug for the product, for example banh-dua. If omitted on upsert, it is derived from product_name.",
			},
			"product_name": map[string]any{
				"type":        "string",
				"description": "Human product name, for example Banh dua.",
			},
			"mcp_server_id": map[string]any{
				"type":        "string",
				"description": "MCP server ID/name for the Google Drive scope.",
			},
			"drive_folder_id": map[string]any{
				"type":        "string",
				"description": "Google Drive product folder ID.",
			},
			"visual_summary": map[string]any{
				"type":        "string",
				"description": "Final visual summary of the product appearance.",
			},
			"packaging_summary": map[string]any{
				"type":        "string",
				"description": "Final packaging/label summary, if visible.",
			},
			"best_reference_file_id": map[string]any{
				"type":        "string",
				"description": "Drive file ID of the best reference image.",
			},
			"asset_versions": map[string]any{
				"type":                 "object",
				"description":          "Map of drive file ID to modified_time:size version from gdrive_get_product_assets.",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required": []string{"action"},
	}
}

func (t *ProductVisualProfileTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.dataDir == "" {
		return ErrorResult("product_visual_profile is not configured: dataDir is empty")
	}
	action, _ := args["action"].(string)
	switch action {
	case "get":
		return t.get(ctx, args)
	case "list":
		return t.list(ctx, args)
	case "upsert":
		return t.upsert(ctx, args)
	case "delete":
		return t.delete(ctx, args)
	default:
		return ErrorResult("action must be one of: get, list, upsert, delete")
	}
}

func (t *ProductVisualProfileTool) get(ctx context.Context, args map[string]any) *Result {
	path, err := t.profilePath(ctx, args, true)
	if err != nil {
		return ErrorResult(err.Error())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewResult(`{"found":false}`)
		}
		return ErrorResult(fmt.Sprintf("read product visual profile: %v", err))
	}
	return NewResult(string(raw))
}

func (t *ProductVisualProfileTool) list(ctx context.Context, args map[string]any) *Result {
	dir, err := t.scopeDir(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}
	var profiles []productVisualProfile
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var profile productVisualProfile
		if json.Unmarshal(raw, &profile) == nil {
			profiles = append(profiles, profile)
		}
		return nil
	})
	slices.SortFunc(profiles, func(a, b productVisualProfile) int {
		return strings.Compare(a.ProductKey, b.ProductKey)
	})
	out, _ := json.MarshalIndent(map[string]any{
		"count":    len(profiles),
		"profiles": profiles,
	}, "", "  ")
	return NewResult(string(out))
}

func (t *ProductVisualProfileTool) upsert(ctx context.Context, args map[string]any) *Result {
	productName, _ := args["product_name"].(string)
	productKey, _ := args["product_key"].(string)
	if productKey == "" {
		productKey = slug(productName)
	}
	if productKey == "" {
		return ErrorResult("product_key or product_name is required for upsert")
	}
	args["product_key"] = productKey

	path, err := t.profilePath(ctx, args, false)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("create product visual profile dir: %v", err))
	}

	mcpServerID, _ := args["mcp_server_id"].(string)
	driveFolderID, _ := args["drive_folder_id"].(string)
	visualSummary, _ := args["visual_summary"].(string)
	packagingSummary, _ := args["packaging_summary"].(string)
	bestReferenceFileID, _ := args["best_reference_file_id"].(string)

	profile := productVisualProfile{
		ProductKey:          productKey,
		ProductName:         productName,
		MCPServerID:         mcpServerID,
		DriveFolderID:       driveFolderID,
		VisualSummary:       visualSummary,
		PackagingSummary:    packagingSummary,
		BestReferenceFileID: bestReferenceFileID,
		AssetVersions:       stringMap(args["asset_versions"]),
		UpdatedAt:           time.Now().UTC().Format(time.RFC3339),
	}

	raw, _ := json.MarshalIndent(profile, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("write product visual profile: %v", err))
	}
	if err := os.Rename(tmp, path); err != nil {
		return ErrorResult(fmt.Sprintf("save product visual profile: %v", err))
	}
	return NewResult(string(raw))
}

func (t *ProductVisualProfileTool) delete(ctx context.Context, args map[string]any) *Result {
	path, err := t.profilePath(ctx, args, true)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("delete product visual profile: %v", err))
	}
	return NewResult(`{"deleted":true}`)
}

func (t *ProductVisualProfileTool) scopeDir(ctx context.Context, args map[string]any) (string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	tenant := tenantID.String()
	if tenantID == store.MasterTenantID || tenantID == uuid.Nil {
		tenant = "master"
	}
	mcpServerID, _ := args["mcp_server_id"].(string)
	if mcpServerID == "" {
		mcpServerID = "unknown-mcp"
	}
	return filepath.Join(t.dataDir, "product-visual-profiles", safePathSegment(tenant), safePathSegment(mcpServerID)), nil
}

func (t *ProductVisualProfileTool) profilePath(ctx context.Context, args map[string]any, requireKey bool) (string, error) {
	dir, err := t.scopeDir(ctx, args)
	if err != nil {
		return "", err
	}
	driveFolderID, _ := args["drive_folder_id"].(string)
	if driveFolderID == "" {
		return "", fmt.Errorf("drive_folder_id is required")
	}
	productKey, _ := args["product_key"].(string)
	if productKey == "" {
		productName, _ := args["product_name"].(string)
		productKey = slug(productName)
	}
	if requireKey && productKey == "" {
		return "", fmt.Errorf("product_key or product_name is required")
	}
	if productKey == "" {
		productKey = "profile"
	}
	return filepath.Join(dir, safePathSegment(driveFolderID), safePathSegment(productKey)+".json"), nil
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	if raw, ok := v.(map[string]any); ok {
		for k, val := range raw {
			if s, ok := val.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	if raw, ok := v.(map[string]string); ok {
		for k, val := range raw {
			if val != "" {
				out[k] = val
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
	value = strings.Trim(value, "._ ")
	if value == "" {
		return "item"
	}
	return value
}
