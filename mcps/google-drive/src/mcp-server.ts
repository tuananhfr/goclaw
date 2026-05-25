import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { DriveConfig } from "./types.js";
import { DriveClient } from "./drive-client.js";
import { DriveAssetCache } from "./cache.js";
import { registerDriveTools } from "./tools.js";

export function createMcpServer(config: DriveConfig): McpServer {
  const server = new McpServer({
    name: "google-drive-mcp",
    version: "1.0.0",
  });

  const client = new DriveClient(config);
  const cache = new DriveAssetCache(config);
  registerDriveTools(server, config, client, cache);

  return server;
}
