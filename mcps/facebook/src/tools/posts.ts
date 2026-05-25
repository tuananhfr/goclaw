/**
 * MCP Tool registrations — Posts domain.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { GraphAPIError } from "../graph-client.js";
import { PageRegistry } from "../page-registry.js";

const postCommentsSchema = z.object({
  comments: z.array(z.union([
    z.string(),
    z.object({
      message: z.string(),
      rationale: z.string().optional(),
    }),
  ])).optional(),
}).passthrough().optional().describe("Optional final comments for GoClaw to schedule after this post. Facebook MCP ignores this field.");

function errorResult(err: unknown, pageId?: string) {
  const msg = err instanceof GraphAPIError ? err.message : String(err);
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `❌ Error: ${prefix}${msg}` }], isError: true };
}

function ok(data: unknown, label: string, pageId?: string) {
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `✅ ${prefix}${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

export function registerPostTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_create_post",
    "Create a new text post on the Facebook Page. Optionally attach a URL link.",
    {
      message: z.string().describe("Post message/content"),
      link: z.string().url().optional().describe("Optional URL to attach to the post"),
      post_comments: postCommentsSchema,
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ message, link, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.createPost(message, link);
        return ok(result, `Post created — ID: ${result.id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_create_post_with_media",
    "Create a post with one or more previously uploaded photos (use fb_upload_photo first to get media IDs).",
    {
      message: z.string().describe("Post message/content"),
      media_ids: z.array(z.string()).min(1).describe("Array of photo IDs from fb_upload_photo"),
      post_comments: postCommentsSchema,
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ message, media_ids, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.createPostWithMedia(message, media_ids);
        return ok(result, `Post with ${media_ids.length} photo(s) created — ID: ${result.id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_edit_post",
    "Edit the message text of an existing post. Note: Facebook does not allow editing attached media.",
    {
      post_id: z.string().describe("The post ID to edit (format: pageId_postId)"),
      message: z.string().describe("New message content"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ post_id, message, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.editPost(post_id, message);
        return ok(result, `Post ${post_id} updated`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_delete_post",
    "Permanently delete a post from the Facebook Page.",
    {
      post_id: z.string().describe("The post ID to delete"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ post_id, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.deletePost(post_id);
        return ok(result, `Post ${post_id} deleted`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_schedule_post",
    "Schedule a post for future publication. The post will be automatically published at the specified time.",
    {
      message: z.string().describe("Post message/content"),
      publish_time: z.number().int().describe("Unix timestamp (seconds) for scheduled publication. Must be 10 min to 75 days in the future."),
      link: z.string().url().optional().describe("Optional URL to attach"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ message, publish_time, link, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.schedulePost(message, publish_time, link);
        const date = new Date(publish_time * 1000).toISOString();
        return ok(result, `Post scheduled for ${date} — ID: ${result.id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_get_posts",
    "Retrieve recent posts from the Facebook Page.",
    {
      limit: z.number().int().min(1).max(100).default(10).describe("Number of posts to fetch (1-100)"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ limit, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.getPosts(limit);
        const posts = result.data ?? [];
        const summary = posts.map((p: any) => ({
          id: p.id,
          message: p.message?.substring(0, 100),
          created: p.created_time,
          url: p.permalink_url,
        }));
        return ok({ count: posts.length, posts: summary }, `Found ${posts.length} posts`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );
}
