/**
 * MCP Server factory — creates a new McpServer instance per SSE connection.
 * Each instance has its own PageRegistry bound to the session.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { PageRegistry } from "./page-registry.js";
import { registerPageTools } from "./tools/pages.js";
import { registerPostTools } from "./tools/posts.js";
import { registerMediaTools } from "./tools/media.js";
import { registerCreativeTools } from "./tools/creative.js";
import { registerCommentTools } from "./tools/comments.js";
import { registerInsightTools } from "./tools/insights.js";
import type { WatermarkConfig } from "./watermark.js";

export interface CommentScheduleConfig {
  enabled?: boolean;
  comment_count?: number;
  window_ms?: number;
  min_gap_ms?: number;
  random_order?: boolean;
}

export interface PageConfig {
  id: string;
  token: string;
  name?: string;
  watermark?: WatermarkConfig;
  commentSchedule?: CommentScheduleConfig;
}

export interface GoclawContext {
  goclawBaseUrl?: string;
  goclawToken?: string;
}

export function createMcpServer(pages: PageConfig[], context?: GoclawContext): McpServer {
  const server = new McpServer({
    name: "facebook-mcp",
    version: "1.0.0",
  });

  const registry = new PageRegistry(context);
  
  for (const page of pages) {
    if (page.id && page.token) {
      registry.addPageSync(page.id, page.token, page.name || `Page ${page.id}`, page.watermark, page.commentSchedule);
    }
  }

  registerPageTools(server, registry);
  registerPostTools(server, registry);
  registerMediaTools(server, registry);
  registerCreativeTools(server, registry);
  registerCommentTools(server, registry);
  registerInsightTools(server, registry);

  return server;
}
