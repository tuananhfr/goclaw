package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeFBMCPTool struct {
	name string
	out  string
	args map[string]any
}

func (t *fakeFBMCPTool) Name() string        { return t.name }
func (t *fakeFBMCPTool) Description() string { return "fake facebook mcp tool" }
func (t *fakeFBMCPTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *fakeFBMCPTool) Execute(_ context.Context, args map[string]any) *Result {
	t.args = args
	return NewResult(t.out)
}

func TestFacebookPostWithComments_SchedulesComments(t *testing.T) {
	reg := NewRegistry()
	postTool := &fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `ok

{"id":"123_456"}`}
	commentTool := &fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`}
	reg.Register(postTool)
	reg.Register(commentTool)
	cron := &fakeCronStore{}
	tool := NewFacebookPostWithCommentsTool(reg, cron)

	before := time.Now().UnixMilli()
	result := tool.Execute(context.Background(), map[string]any{
		"post_kind": "photo",
		"page_id":   "page-1",
		"post_args": map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{
			"enabled":      true,
			"window_ms":    float64(30 * 60 * 1000),
			"random_order": false,
			"comments": []any{
				map[string]any{"message": "Comment one", "rationale": "r1"},
				map[string]any{"message": "Comment two", "rationale": "r2"},
			},
		},
	})
	after := time.Now().Add(30 * time.Minute).UnixMilli()

	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if len(cron.toolCallJobs) != 2 {
		t.Fatalf("scheduled jobs = %d, want 2", len(cron.toolCallJobs))
	}
	if postTool.args["page_id"] != "page-1" {
		t.Fatalf("page_id was not forwarded to post tool: %#v", postTool.args)
	}
	for _, job := range cron.toolCallJobs {
		if job.toolName != "mcp_fb__fb_create_post_comment" {
			t.Fatalf("toolName = %q", job.toolName)
		}
		if job.args["post_id"] != "123_456" || job.args["page_id"] != "page-1" {
			t.Fatalf("bad comment args: %#v", job.args)
		}
		if job.atMS < before || job.atMS > after {
			t.Fatalf("run time %d outside expected window [%d,%d]", job.atMS, before, after)
		}
	}
}

func TestFacebookPostWithComments_ExecuteUsesCallingRegistry(t *testing.T) {
	base := NewRegistry()
	cron := &fakeCronStore{}
	base.Register(NewFacebookPostWithCommentsTool(base, cron))

	clone := base.Clone()
	clone.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`})
	clone.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})

	result := clone.ExecuteWithContext(context.Background(), "facebook_post_with_comments", map[string]any{
		"post_kind":             "photo",
		"mcp_post_tool_name":    "mcp_fb__fb_create_photo_post",
		"mcp_comment_tool_name": "mcp_fb__fb_create_post_comment",
		"post_args":             map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{
			"enabled":   true,
			"window_ms": float64(30 * 60 * 1000),
			"comments":  []any{"Comment one", "Comment two"},
		},
	}, "", "", "", "s1", nil)

	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if len(cron.toolCallJobs) != 2 {
		t.Fatalf("scheduled jobs = %d, want 2", len(cron.toolCallJobs))
	}
}

func TestFacebookPostWithComments_DisabledCommentsOnlyPosts(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})
	cron := &fakeCronStore{}
	tool := NewFacebookPostWithCommentsTool(reg, cron)

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind":     "photo",
		"post_args":     map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{"enabled": false},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if len(cron.toolCallJobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0", len(cron.toolCallJobs))
	}
}

func TestFacebookPostWithComments_ReadsMCPCommentSchedulePolicy(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_get_comment_schedule_config", out: `ok

{"enabled":true,"comment_schedule":{"enabled":true,"comment_count":2,"window_ms":1800000,"min_gap_ms":60000,"random_order":false}}`})
	cron := &fakeCronStore{}
	tool := NewFacebookPostWithCommentsTool(reg, cron)

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind":     "photo",
		"page_id":       "page-1",
		"post_args":     map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{"comments": []any{"Context comment one", "Context comment two"}},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if len(cron.toolCallJobs) != 2 {
		t.Fatalf("scheduled jobs = %d, want 2", len(cron.toolCallJobs))
	}
}

func TestFacebookPostWithComments_MCPPolicyRequiresFinalComments(t *testing.T) {
	reg := NewRegistry()
	postTool := &fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`}
	reg.Register(postTool)
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_get_comment_schedule_config", out: `{"enabled":true,"comment_schedule":{"enabled":true,"comment_count":2,"window_ms":1800000}}`})
	tool := NewFacebookPostWithCommentsTool(reg, &fakeCronStore{})

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind": "photo",
		"post_args": map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "no final comments") {
		t.Fatalf("expected missing final comments error, got: %#v", result)
	}
	if postTool.args != nil {
		t.Fatalf("post tool should not be called before final comments are available")
	}
}

func TestFacebookPostWithComments_AutoHookBlocksMCPPostWithoutComments(t *testing.T) {
	reg := NewRegistry()
	postTool := &fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`}
	reg.Register(postTool)
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_get_comment_schedule_config", out: `{"enabled":true,"comment_schedule":{"enabled":true,"comment_count":2,"window_ms":1800000}}`})
	tool := NewFacebookPostWithCommentsTool(reg, &fakeCronStore{})
	reg.AddBeforeExecuteHook(tool.BeforeExecute)
	reg.AddAfterExecuteHook(tool.AfterExecute)

	result := reg.ExecuteWithContext(context.Background(), "mcp_fb__fb_create_photo_post", map[string]any{
		"image_url": "MEDIA:/x.jpg",
		"caption":   "caption",
	}, "", "", "", "s1", nil)

	if !result.IsError || !strings.Contains(result.ForLLM, "no final comments") {
		t.Fatalf("expected pre-post final comment error, got: %#v", result)
	}
	if postTool.args != nil {
		t.Fatalf("post tool should not be called")
	}
}

func TestFacebookPostWithComments_AutoHookUsesClonedRegistryMCPTools(t *testing.T) {
	base := NewRegistry()
	tool := NewFacebookPostWithCommentsTool(base, &fakeCronStore{})
	base.AddBeforeExecuteHook(tool.BeforeExecute)
	base.AddAfterExecuteHook(tool.AfterExecute)

	clone := base.Clone()
	postTool := &fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`}
	clone.Register(postTool)
	clone.Register(&fakeFBMCPTool{name: "mcp_fb__fb_get_comment_schedule_config", out: `{"enabled":true,"comment_schedule":{"enabled":true,"comment_count":2,"window_ms":1800000}}`})

	result := clone.ExecuteWithContext(context.Background(), "mcp_fb__fb_create_photo_post", map[string]any{
		"image_url": "MEDIA:/x.jpg",
		"caption":   "caption",
	}, "", "", "", "s1", nil)

	if !result.IsError || !strings.Contains(result.ForLLM, "no final comments") {
		t.Fatalf("expected cloned registry hook to read MCP policy and block, got: %#v", result)
	}
	if postTool.args != nil {
		t.Fatalf("post tool should not be called")
	}
}

func TestFacebookPostWithComments_AutoHookSchedulesAfterDirectMCPPost(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123_456"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_get_comment_schedule_config", out: `{"enabled":true,"comment_schedule":{"enabled":true,"comment_count":2,"window_ms":1800000,"random_order":false}}`})
	cron := &fakeCronStore{}
	tool := NewFacebookPostWithCommentsTool(reg, cron)
	reg.AddBeforeExecuteHook(tool.BeforeExecute)
	reg.AddAfterExecuteHook(tool.AfterExecute)

	result := reg.ExecuteWithContext(context.Background(), "mcp_fb__fb_create_photo_post", map[string]any{
		"image_url": "MEDIA:/x.jpg",
		"caption":   "caption",
		"post_comments": map[string]any{
			"comments": []any{"Context comment one", "Context comment two"},
		},
	}, "", "", "", "s1", nil)

	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if len(cron.toolCallJobs) != 2 {
		t.Fatalf("scheduled jobs = %d, want 2", len(cron.toolCallJobs))
	}
	if !strings.Contains(result.ForLLM, "GoClaw scheduled Facebook comments") {
		t.Fatalf("result did not include scheduling summary: %s", result.ForLLM)
	}
}

func TestFacebookPostWithComments_MissingPostIDDoesNotSchedule(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"ok":true}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})
	cron := &fakeCronStore{}
	tool := NewFacebookPostWithCommentsTool(reg, cron)

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind": "photo",
		"post_args": map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{
			"enabled":   true,
			"window_ms": float64(1000),
			"comments":  []any{"Comment one"},
		},
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "post_id") {
		t.Fatalf("expected missing post id error, got: %#v", result)
	}
	if len(cron.toolCallJobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0", len(cron.toolCallJobs))
	}
}

func TestFacebookPostWithComments_MultipleMCPToolsRequireOverride(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_a__fb_create_photo_post", out: `{"id":"123"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_b__fb_create_photo_post", out: `{"id":"123"}`})
	tool := NewFacebookPostWithCommentsTool(reg, &fakeCronStore{})

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind": "photo",
		"post_args": map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "multiple MCP tools") {
		t.Fatalf("expected multiple tools error, got: %#v", result)
	}
}

func TestFacebookPostWithComments_InvalidCommentPlan(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_photo_post", out: `{"id":"123"}`})
	reg.Register(&fakeFBMCPTool{name: "mcp_fb__fb_create_post_comment", out: `{"id":"c1"}`})
	tool := NewFacebookPostWithCommentsTool(reg, &fakeCronStore{})

	result := tool.Execute(context.Background(), map[string]any{
		"post_kind": "photo",
		"post_args": map[string]any{"image_url": "MEDIA:/x.jpg", "caption": "caption"},
		"post_comments": map[string]any{
			"enabled":    true,
			"window_ms":  float64(1000),
			"min_gap_ms": float64(1000),
			"comments": []any{
				"G\u1ee3i \u00fd reply: bad",
				"Another comment",
			},
		},
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "internal draft marker") {
		t.Fatalf("expected invalid comment error, got: %#v", result)
	}
}
