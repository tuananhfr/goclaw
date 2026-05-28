package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestProcessToolResultApplyWatermarkReplacesBaseMedia(t *testing.T) {
	workspace := t.TempDir()
	basePath := filepath.Join(workspace, "generated", "post.png")
	watermarkedPath := filepath.Join(workspace, "generated", "post-watermarked.jpg")
	rs := &runState{
		mediaResults: []MediaResult{{Path: basePath, ContentType: "image/png"}},
	}
	loop := &Loop{id: "test-agent"}
	ctx := tools.WithToolWorkspace(context.Background(), workspace)

	loop.processToolResult(
		ctx,
		rs,
		&RunRequest{RunID: "run-1"},
		func(AgentEvent) {},
		providers.ToolCall{
			ID:        "tc-1",
			Name:      "apply_watermark",
			Arguments: map[string]any{"base_image_path": "MEDIA:generated/post.png"},
		},
		"apply_watermark",
		&tools.Result{
			ForLLM: "has_watermark: true",
			Media:  []bus.MediaFile{{Path: watermarkedPath, MimeType: "image/jpeg"}},
		},
		false,
	)

	if len(rs.mediaResults) != 1 {
		t.Fatalf("mediaResults len = %d, want 1: %#v", len(rs.mediaResults), rs.mediaResults)
	}
	if got := rs.mediaResults[0].Path; got != watermarkedPath {
		t.Fatalf("remaining media path = %q, want %q", got, watermarkedPath)
	}
}

func TestProcessToolResultApplyWatermarkReplacesTeamWorkspaceBaseMedia(t *testing.T) {
	agentWorkspace := t.TempDir()
	teamWorkspace := t.TempDir()
	basePath := filepath.Join(teamWorkspace, "generated", "post.png")
	watermarkedPath := filepath.Join(agentWorkspace, "final", "post.png")
	rs := &runState{
		mediaResults: []MediaResult{{Path: basePath, ContentType: "image/png"}},
	}
	loop := &Loop{id: "test-agent"}
	ctx := tools.WithToolWorkspace(context.Background(), agentWorkspace)
	ctx = tools.WithToolTeamWorkspace(ctx, teamWorkspace)

	loop.processToolResult(
		ctx,
		rs,
		&RunRequest{RunID: "run-1"},
		func(AgentEvent) {},
		providers.ToolCall{
			ID:        "tc-1",
			Name:      "apply_watermark",
			Arguments: map[string]any{"base_image_path": "MEDIA:generated/post.png"},
		},
		"apply_watermark",
		&tools.Result{
			ForLLM: "has_watermark: true",
			Media:  []bus.MediaFile{{Path: watermarkedPath, MimeType: "image/png"}},
		},
		false,
	)

	if len(rs.mediaResults) != 1 {
		t.Fatalf("mediaResults len = %d, want 1: %#v", len(rs.mediaResults), rs.mediaResults)
	}
	if got := rs.mediaResults[0].Path; got != watermarkedPath {
		t.Fatalf("remaining media path = %q, want %q", got, watermarkedPath)
	}
}

func TestProcessToolResultMCPApplyWatermarkReplacesImageURLBaseMedia(t *testing.T) {
	workspace := t.TempDir()
	basePath := filepath.Join(workspace, "generated", "post.png")
	watermarkedPath := filepath.Join(workspace, "generated", "mcp", "fb_apply_watermark-abcd.jpg")
	rs := &runState{
		mediaResults: []MediaResult{{Path: basePath, ContentType: "image/png"}},
	}
	loop := &Loop{id: "test-agent"}
	ctx := tools.WithToolWorkspace(context.Background(), workspace)

	loop.processToolResult(
		ctx,
		rs,
		&RunRequest{RunID: "run-1"},
		func(AgentEvent) {},
		providers.ToolCall{
			ID:        "tc-1",
			Name:      "mcp_page_facebook__fb_apply_watermark",
			Arguments: map[string]any{"image_url": "generated/post.png"},
		},
		"mcp_page_facebook__fb_apply_watermark",
		&tools.Result{
			ForLLM: "MEDIA:" + watermarkedPath,
			Media:  []bus.MediaFile{{Path: watermarkedPath, MimeType: "image/jpeg"}},
		},
		false,
	)

	if len(rs.mediaResults) != 1 {
		t.Fatalf("mediaResults len = %d, want 1: %#v", len(rs.mediaResults), rs.mediaResults)
	}
	if got := rs.mediaResults[0].Path; got != watermarkedPath {
		t.Fatalf("remaining media path = %q, want %q", got, watermarkedPath)
	}
}
