package cmd

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type cronFakeTool struct {
	name   string
	called int
	args   map[string]any
}

func (t *cronFakeTool) Name() string        { return t.name }
func (t *cronFakeTool) Description() string { return "fake" }
func (t *cronFakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *cronFakeTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	t.called++
	t.args = args
	return tools.NewResult("ok")
}

func TestRunCronToolCall(t *testing.T) {
	reg := tools.NewRegistry()
	tool := &cronFakeTool{name: "mcp_fb__fb_create_post_comment"}
	reg.Register(tool)

	job := &store.CronJob{
		ID:     "job-1",
		UserID: "user-1",
		Payload: store.CronPayload{
			Kind:     "tool_call",
			ToolName: "mcp_fb__fb_create_post_comment",
			Args: map[string]any{
				"post_id": "123_456",
				"message": "hello",
			},
		},
	}

	result, err := runCronToolCall(job, &config.Config{}, reg)
	if err != nil {
		t.Fatalf("runCronToolCall returned error: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q, want ok", result.Content)
	}
	if tool.called != 1 {
		t.Fatalf("tool calls = %d, want 1", tool.called)
	}
	if tool.args["post_id"] != "123_456" || tool.args["message"] != "hello" {
		t.Fatalf("unexpected args: %#v", tool.args)
	}
}
