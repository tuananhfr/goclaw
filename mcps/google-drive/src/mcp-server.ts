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
  void cache.ensureInitialSync(client).catch((err) => {
    console.warn(`[google-drive-mcp] initial sync failed: ${err instanceof Error ? err.message : String(err)}`);
  });
  scheduleDailySync(config, client, cache);

  return server;
}

function scheduleDailySync(config: DriveConfig, client: DriveClient, cache: DriveAssetCache) {
  const [hourRaw, minuteRaw] = config.syncTime.split(":");
  const hour = Number.parseInt(hourRaw || "0", 10);
  const minute = Number.parseInt(minuteRaw || "0", 10);
  const run = () => {
    void cache.syncChanges(client).catch((err) => {
      console.warn(`[google-drive-mcp] scheduled sync failed: ${err instanceof Error ? err.message : String(err)}`);
    });
  };
  const scheduleNext = () => {
    const now = new Date();
    const next = new Date(now);
    next.setHours(Number.isFinite(hour) ? hour : 0, Number.isFinite(minute) ? minute : 0, 0, 0);
    if (next <= now) next.setDate(next.getDate() + 1);
    setTimeout(() => {
      run();
      scheduleNext();
    }, next.getTime() - now.getTime()).unref();
  };
  scheduleNext();
}
