/**
 * MCP Tool registrations — Insights & Page Info domain.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { GraphAPIError } from "../graph-client.js";
import { PageRegistry } from "../page-registry.js";

function errorResult(err: unknown, pageId?: string) {
  const msg = err instanceof GraphAPIError ? err.message : String(err);
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `❌ Error: ${prefix}${msg}` }], isError: true };
}

function ok(data: unknown, label: string, pageId?: string) {
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `✅ ${prefix}${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

export function registerInsightTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_get_post_insights",
    "Get analytics/metrics for a specific post: impressions, reach, engagement, clicks, reactions.",
    {
      post_id: z.string().describe("The post ID to get insights for"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ post_id, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.getPostInsights(post_id);
        const metrics = (result.data ?? []).map((m: any) => ({
          name: m.name,
          value: m.values?.[0]?.value ?? 0,
          description: m.description,
        }));
        return ok({ post_id, metrics }, `Insights for post ${post_id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_get_page_info",
    "Get extended information about the connected Facebook Page: name, category, fan count, website, etc.",
    {
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.getPageInfo();
        return ok(result, `Page info for "${result.name ?? client.getPageId()}"`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_get_rate_limit",
    "Check the current API rate limit usage for this Page. Returns call_count percentage — above 80% means approaching throttle.",
    {
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const info = client.getRateLimit();
        if (!info) {
          return { content: [{ type: "text" as const, text: `ℹ️ [Page: ${client.getPageId()}] No rate limit data yet. Make a Graph API call first.` }] };
        }
        const warning = info.callCount > 80 ? "\n⚠️ WARNING: Approaching rate limit! Slow down API calls." : "";
        return ok(info, `Rate limit usage: ${info.callCount}%${warning}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );
}
