package mcp

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

var onePixelPNG = base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfake"))

func imageWithAudience(roles ...mcpgo.Role) mcpgo.ImageContent {
	img := mcpgo.NewImageContent(onePixelPNG, "image/png")
	if len(roles) > 0 {
		img.Annotations = &mcpgo.Annotations{Audience: roles}
	}
	return img
}

func TestPersistImageContent_AudienceControlsDelivery(t *testing.T) {
	ws := t.TempDir()
	ctx := tools.WithToolWorkspace(context.Background(), ws)
	bridge := &BridgeTool{toolName: "erp_company_branding"}

	cases := []struct {
		name       string
		content    mcpgo.ImageContent
		wantMedia  bool
		wantPrefix string
	}{
		{"no annotations", imageWithAudience(), true, "MEDIA:"},
		{"user audience", imageWithAudience(mcpgo.RoleUser, mcpgo.RoleAssistant), true, "MEDIA:"},
		{"assistant only", imageWithAudience(mcpgo.RoleAssistant), false, "Image saved for your use"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &mcpgo.CallToolResult{Content: []mcpgo.Content{tc.content}}
			media, text := bridge.persistImageContent(ctx, result)

			if got := len(media) == 1; got != tc.wantMedia {
				t.Fatalf("media delivered = %v, want %v (%v)", got, tc.wantMedia, media)
			}
			if !strings.HasPrefix(text, tc.wantPrefix) {
				t.Fatalf("text = %q, want prefix %q", text, tc.wantPrefix)
			}
			path := text[strings.LastIndex(text, " ")+1:]
			path = strings.TrimPrefix(path, "MEDIA:")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("image not saved in workspace: %v", err)
			}
			if !tc.wantMedia && strings.Contains(text, "MEDIA:") {
				t.Fatalf("assistant-only image must not carry a MEDIA: token: %q", text)
			}
		})
	}
}
