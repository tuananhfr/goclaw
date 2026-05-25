/**
 * MCP Tool registrations — Comments domain.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { GraphAPIError } from "../graph-client.js";
import { PageRegistry } from "../page-registry.js";
import type { CommentScheduleConfig } from "../mcp-server.js";

function errorResult(err: unknown, pageId?: string) {
  const msg = err instanceof GraphAPIError ? err.message : String(err);
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `❌ Error: ${prefix}${msg}` }], isError: true };
}

function ok(data: unknown, label: string, pageId?: string) {
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `✅ ${prefix}${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

function hasCommentSchedule(cfg?: CommentScheduleConfig): boolean {
  return cfg?.enabled === true && (cfg.comment_count ?? 0) > 0 && (cfg.window_ms ?? 0) > 0;
}

export function registerCommentTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_get_comment_schedule_config",
    "Get the configured scheduled-comment policy for this Facebook Page. GoClaw should use this policy to generate final context-aware comments before posting and schedule them after the post is created.",
    {
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ page_id }) => {
      try {
        const { pageId, commentSchedule } = registry.getCommentScheduleConfig(page_id);
        return ok(
          {
            page_id: pageId,
            enabled: commentSchedule?.enabled === true,
            has_comment_schedule: hasCommentSchedule(commentSchedule),
            comment_schedule: commentSchedule,
          },
          `Comment schedule config for page ${pageId}`,
          pageId,
        );
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_get_comments",
    "Retrieve comments on a specific post.",
    {
      post_id: z.string().describe("The post ID to get comments for"),
      limit: z.number().int().min(1).max(100).default(25).describe("Number of comments to fetch"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ post_id, limit, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.getComments(post_id, limit);
        const comments = result.data ?? [];
        return ok({ count: comments.length, comments }, `Found ${comments.length} comments`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_create_post_comment",
    "Create a new top-level comment on a specific Page post.",
    {
      post_id: z.string().describe("The post ID to comment on, usually in pageId_postId format"),
      message: z.string().describe("Comment message"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ post_id, message, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.createPostComment(post_id, message);
        return ok(result, `Comment created on post ${post_id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_reply_comment",
    "Reply to a specific comment on a post.",
    {
      comment_id: z.string().describe("The comment ID to reply to"),
      message: z.string().describe("Reply message"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ comment_id, message, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.replyComment(comment_id, message);
        return ok(result, `Replied to comment ${comment_id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_delete_comment",
    "Delete a specific comment.",
    {
      comment_id: z.string().describe("The comment ID to delete"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ comment_id, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.deleteComment(comment_id);
        return ok(result, `Comment ${comment_id} deleted`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_hide_comment",
    "Hide or unhide a comment from public view.",
    {
      comment_id: z.string().describe("The comment ID to hide/unhide"),
      hide: z.boolean().default(true).describe("true to hide, false to unhide"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ comment_id, hide, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.hideComment(comment_id, hide);
        const action = hide ? "hidden" : "unhidden";
        return ok(result, `Comment ${comment_id} ${action}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );
}
