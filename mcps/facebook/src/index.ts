/**
 * Entry point — Express HTTP server with SSE transport for MCP.
 *
 * Architecture (Option B — Connection-Scoped):
 *   1. GoClaw opens SSE connection with `Authorization: Bearer <fb_token>` header
 *   2. Server extracts token + page_id, creates a dedicated McpServer + GraphClient
 *   3. Each connection is fully isolated (own server instance, own token)
 *   4. On disconnect, server instance is cleaned up
 */
import express from "express";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import { createMcpServer, PageConfig, GoclawContext, type CommentScheduleConfig } from "./mcp-server.js";
import type { WatermarkConfig } from "./watermark.js";

const PORT = parseInt(process.env.PORT ?? "3100", 10);

const app = express();

// Active SSE sessions: sessionId → { transport, server }
const sessions = new Map<string, { transport: SSEServerTransport; server: ReturnType<typeof createMcpServer> }>();

function parseWatermark(raw: unknown): WatermarkConfig | undefined {
  if (typeof raw !== "string" || raw.trim() === "") return undefined;
  try {
    return JSON.parse(raw) as WatermarkConfig;
  } catch {
    console.warn(`[SSE] Ignoring invalid watermark config`);
    return undefined;
  }
}

function parseCommentSchedule(raw: unknown): CommentScheduleConfig | undefined {
  if (typeof raw !== "string" || raw.trim() === "") return undefined;
  try {
    return JSON.parse(raw) as CommentScheduleConfig;
  } catch {
    console.warn(`[SSE] Ignoring invalid comment schedule config`);
    return undefined;
  }
}

// Helper to extract pages from HTTP Headers (GoClaw config) or ENV
function extractPages(headers: Record<string, any>, env: NodeJS.ProcessEnv): PageConfig[] {
  const pages: PageConfig[] = [];
  
  // 1. Support legacy default page
  const authHeader = headers.authorization ?? "";
  const defaultToken = authHeader.startsWith("Bearer ") ? authHeader.slice(7) : (env.FB_PAGE_ACCESS_TOKEN ?? "");
  const defaultPageId = (headers["x-facebook-page-id"] as string) ?? (env.FB_PAGE_ID ?? "");
  const defaultPageName = (headers["x-facebook-page-name"] as string) ?? "Default Page";
  const defaultWatermark = parseWatermark(headers["x-facebook-watermark"] ?? env.FB_WATERMARK);
  const defaultCommentSchedule = parseCommentSchedule(headers["x-facebook-comment-schedule"] ?? env.FB_COMMENT_SCHEDULE);
  
  if (defaultToken && defaultPageId) {
    pages.push({ id: defaultPageId, token: defaultToken, name: defaultPageName, watermark: defaultWatermark, commentSchedule: defaultCommentSchedule });
  }

  // 2. Support Option B: Numbered config from Headers (x-fb-page-1-id) or Env (FB_PAGE_1_ID)
  for (let i = 1; i <= 20; i++) {
    const id = (headers[`x-fb-page-${i}-id`] as string) || env[`FB_PAGE_${i}_ID`];
    const token = (headers[`x-fb-page-${i}-token`] as string) || env[`FB_PAGE_${i}_TOKEN`];
    const name = (headers[`x-fb-page-${i}-name`] as string) || env[`FB_PAGE_${i}_NAME`];
    const watermark = parseWatermark(headers[`x-fb-page-${i}-watermark`] || env[`FB_PAGE_${i}_WATERMARK`]);
    const commentSchedule = parseCommentSchedule(headers[`x-fb-page-${i}-comment-schedule`] || env[`FB_PAGE_${i}_COMMENT_SCHEDULE`]);
    
    if (id && token) {
      // Avoid duplicates
      if (!pages.some(p => p.id === id)) {
        pages.push({ id, token, name: name || `Page ${i}`, watermark, commentSchedule });
      }
    }
  }

  return pages;
}

// ── Health check ──
app.get("/health", (_req, res) => {
  res.json({
    status: "ok",
    activeSessions: sessions.size,
    uptime: process.uptime(),
  });
});

// ── SSE endpoint — GoClaw connects here ──
app.get("/sse", async (req, res) => {
  // Extract all pages from headers and env
  const pages = extractPages(req.headers, process.env);

  console.log(`[SSE] New connection. Extracted ${pages.length} pages from config.`);
  pages.forEach(p => {
    const wm = p.watermark;
    const ref = wm?.logo_url || wm?.logo_path || (wm?.mode === "text" ? "text" : "");
    const cs = p.commentSchedule;
    console.log(`      - Registered Page: ${p.id} (${p.name}) watermark=${wm?.enabled ? "enabled" : "disabled"} items=${wm?.items?.length ?? (wm?.enabled ? 1 : 0)} mode=${wm?.mode ?? "-"} ref=${ref ? "set" : "-"} comment_schedule=${cs?.enabled ? "enabled" : "disabled"} count=${cs?.comment_count ?? 0} window_ms=${cs?.window_ms ?? 0}`);
  });

  if (pages.length === 0) {
    console.warn(`[SSE] Tools will fail when called since no pages were provided in config.`);
  }

  const goclawBaseUrl = (req.headers["x-goclaw-base-url"] as string) || process.env.GOCLAW_BASE_URL;
  const goclawToken = (req.headers["x-goclaw-gateway-token"] as string) || process.env.GOCLAW_GATEWAY_TOKEN;

  // Create isolated MCP server for this connection
  const mcpServer = createMcpServer(pages, { goclawBaseUrl, goclawToken });
  const transport = new SSEServerTransport("/messages", res);

  sessions.set(transport.sessionId, { transport, server: mcpServer });
  console.log(`[SSE] Session created: ${transport.sessionId}`);

  // Cleanup on disconnect
  res.on("close", () => {
    console.log(`[SSE] Disconnected — Session: ${transport.sessionId}`);
    sessions.delete(transport.sessionId);
    mcpServer.close().catch(() => {});
  });

  try {
    await mcpServer.connect(transport);
  } catch (err) {
    console.error(`[SSE] Connect error:`, err);
    sessions.delete(transport.sessionId);
  }
});

// ── JSON-RPC message endpoint ──
// IMPORTANT: No body-parsing middleware here — SSEServerTransport reads the raw stream.
app.post("/messages", async (req, res) => {
  const sessionId = req.query.sessionId as string;
  console.log(`[MSG] Received message for session: ${sessionId}`);

  const entry = sessions.get(sessionId);
  if (!entry) {
    res.status(404).json({ error: "Session not found", sessionId });
    return;
  }

  try {
    await entry.transport.handlePostMessage(req, res);
  } catch (err) {
    console.error(`[MSG] Error handling message:`, err);
    res.status(500).json({ error: "Internal error" });
  }
});

// ── Start server ──
app.listen(PORT, () => {
  console.log(`
╔══════════════════════════════════════════════╗
║   Facebook MCP Server                        ║
║   Port: ${PORT}                                  ║
║   Transport: SSE                             ║
║   Endpoint: http://localhost:${PORT}/sse          ║
╚══════════════════════════════════════════════╝

Waiting for MCP client connections...
  `);
});
