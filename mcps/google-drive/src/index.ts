/**
 * Google Drive MCP entrypoint.
 *
 * One server config is one folder scope. The MCP receives OAuth credentials and
 * ROOT_FOLDER_ID through GoClaw MCP headers or container env, then exposes only
 * tools that operate inside that root subtree.
 */
import express from "express";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import { createMcpServer } from "./mcp-server.js";
import type { DriveConfig } from "./types.js";
import { boolEnvOrHeader, envOrHeader, intEnvOrHeader, jsonEnvOrHeader } from "./utils.js";

const PORT = Number.parseInt(process.env.PORT ?? "3200", 10);
const app = express();

const sessions = new Map<string, { transport: SSEServerTransport; server: ReturnType<typeof createMcpServer> }>();

function extractConfig(headers: Record<string, any>, env: NodeJS.ProcessEnv): DriveConfig {
  return {
    clientId: envOrHeader(headers, env, "x-gdrive-client-id", "GOOGLE_DRIVE_CLIENT_ID"),
    clientSecret: envOrHeader(headers, env, "x-gdrive-client-secret", "GOOGLE_DRIVE_CLIENT_SECRET"),
    refreshToken: envOrHeader(headers, env, "x-gdrive-refresh-token", "GOOGLE_DRIVE_REFRESH_TOKEN"),
    rootFolderId: envOrHeader(headers, env, "x-gdrive-root-folder-id", "GOOGLE_DRIVE_ROOT_FOLDER_ID"),
    rootFolderName: envOrHeader(headers, env, "x-gdrive-root-folder-name", "GOOGLE_DRIVE_ROOT_FOLDER_NAME") || undefined,
    cacheDir: envOrHeader(headers, env, "x-gdrive-cache-dir", "GOOGLE_DRIVE_CACHE_DIR") || "/app/workspace/drive-cache",
    cacheTTLSeconds: intEnvOrHeader(headers, env, "x-gdrive-cache-ttl-seconds", "GOOGLE_DRIVE_CACHE_TTL_SECONDS", 300),
    maxAssets: intEnvOrHeader(headers, env, "x-gdrive-max-assets", "GOOGLE_DRIVE_MAX_ASSETS", 50),
    agentFolderGrants: jsonEnvOrHeader<Record<string, string[]>>(headers, env, "x-gdrive-agent-folder-grants", "GOOGLE_DRIVE_AGENT_FOLDER_GRANTS", {}),
    allowPublicLinkImport: boolEnvOrHeader(headers, env, "x-gdrive-allow-public-link-import", "GOOGLE_DRIVE_ALLOW_PUBLIC_LINK_IMPORT", true),
    syncTime: envOrHeader(headers, env, "x-gdrive-sync-time", "GOOGLE_DRIVE_SYNC_TIME") || "00:00",
    timezone: envOrHeader(headers, env, "x-gdrive-timezone", "GOOGLE_DRIVE_TIMEZONE") || "Asia/Ho_Chi_Minh",
  };
}

function validateConfig(config: DriveConfig): string[] {
  const missing: string[] = [];
  if (!config.clientId) missing.push("GOOGLE_DRIVE_CLIENT_ID / x-gdrive-client-id");
  if (!config.clientSecret) missing.push("GOOGLE_DRIVE_CLIENT_SECRET / x-gdrive-client-secret");
  if (!config.refreshToken) missing.push("GOOGLE_DRIVE_REFRESH_TOKEN / x-gdrive-refresh-token");
  if (!config.rootFolderId) missing.push("GOOGLE_DRIVE_ROOT_FOLDER_ID / x-gdrive-root-folder-id");
  return missing;
}

app.get("/health", (_req, res) => {
  res.json({
    status: "ok",
    activeSessions: sessions.size,
    uptime: process.uptime(),
  });
});

app.get("/sse", async (req, res) => {
  const config = extractConfig(req.headers, process.env);
  const missing = validateConfig(config);
  if (missing.length > 0) {
    console.warn(`[SSE] Google Drive MCP missing config: ${missing.join(", ")}`);
  } else {
    console.log(`[SSE] Google Drive MCP connection root=${config.rootFolderId} cache=${config.cacheDir}`);
  }

  const mcpServer = createMcpServer(config);
  const transport = new SSEServerTransport("/messages", res);
  sessions.set(transport.sessionId, { transport, server: mcpServer });

  res.on("close", () => {
    sessions.delete(transport.sessionId);
    mcpServer.close().catch(() => {});
  });

  try {
    await mcpServer.connect(transport);
  } catch (err) {
    console.error("[SSE] Connect error:", err);
    sessions.delete(transport.sessionId);
  }
});

app.post("/messages", async (req, res) => {
  const sessionId = req.query.sessionId as string;
  const entry = sessions.get(sessionId);
  if (!entry) {
    res.status(404).json({ error: "Session not found", sessionId });
    return;
  }
  try {
    await entry.transport.handlePostMessage(req, res);
  } catch (err) {
    console.error("[MSG] Error handling message:", err);
    res.status(500).json({ error: "Internal error" });
  }
});

app.listen(PORT, () => {
  console.log([
    "Google Drive MCP Server",
    `Port: ${PORT}`,
    "Transport: SSE",
    `Endpoint: http://localhost:${PORT}/sse`,
    "Waiting for MCP client connections...",
  ].join("\n"));
});
