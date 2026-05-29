import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import type { DriveAssetCache } from "./cache.js";
import type { DriveClient } from "./drive-client.js";
import type { DriveConfig, DriveFile } from "./types.js";
import { parseDriveID } from "./utils.js";

const internalAgentFields = {
  goclaw_agent_key: z.string().optional().describe("Internal GoClaw agent key injected by the bridge."),
  goclaw_agent_id: z.string().optional().describe("Internal GoClaw agent UUID injected by the bridge."),
};

function ok(data: unknown, label: string) {
  return { content: [{ type: "text" as const, text: `OK: ${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

function errorResult(err: unknown) {
  return { content: [{ type: "text" as const, text: `Error: ${err instanceof Error ? err.message : String(err)}` }], isError: true };
}

export function registerDriveTools(server: McpServer, config: DriveConfig, client: DriveClient, cache: DriveAssetCache) {
  server.tool(
    "gdrive_health",
    "Check Google Drive OAuth credentials, root folder, index and sync status.",
    {},
    async () => {
      try {
        return ok({ ...(await client.health()), sync: await cache.status() }, "Google Drive connection is healthy");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_list_allowed_folders",
    "List Google Drive folders granted to the current GoClaw agent.",
    internalAgentFields,
    async ({ goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const folders = await cache.allowedFolders(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        return ok({ agent_key: goclaw_agent_key, agent_id: goclaw_agent_id, folders }, `Found ${folders.length} allowed folders`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_search",
    "Search cached Google Drive files inside folders granted to the current GoClaw agent.",
    {
      query: z.string().optional().describe("Filename or keyword to search in the local Drive index."),
      limit: z.number().int().positive().optional().describe("Maximum results"),
      ...internalAgentFields,
    },
    async ({ query, limit, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        const assets = await cache.search(query ?? "", allowed, Math.min(Math.max(limit ?? 20, 1), config.maxAssets));
        return ok({ count: assets.length, assets }, `Found ${assets.length} cached Drive files`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_list_folder",
    "List a cached Google Drive folder if it is granted to the current GoClaw agent.",
    {
      folder_id_or_url: z.string().describe("Google Drive folder ID or URL"),
      recursive: z.boolean().optional().describe("Include descendants"),
      limit: z.number().int().positive().optional(),
      ...internalAgentFields,
    },
    async ({ folder_id_or_url, recursive, limit, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const folderId = parseDriveID(folder_id_or_url);
        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        const items = await cache.listFolder(folderId, allowed, recursive ?? false, Math.min(Math.max(limit ?? 100, 1), config.maxAssets * 5));
        return ok({ folder_id: folderId, count: items.length, items }, `Listed ${items.length} Drive items`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_download",
    "Download/cache one Google Drive file after verifying it belongs to a folder granted to the current GoClaw agent.",
    {
      file_id_or_url: z.string().describe("Google Drive file ID or URL"),
      ...internalAgentFields,
    },
    async ({ file_id_or_url, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const fileId = parseDriveID(file_id_or_url);
        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        const indexed = await cache.assertFileAllowed(fileId, allowed);
        const file: DriveFile = {
          id: indexed.drive_file_id,
          name: indexed.name,
          mimeType: indexed.mime_type,
          parents: indexed.parents,
          modifiedTime: indexed.modified_time,
          md5Checksum: indexed.md5_checksum,
          size: indexed.size ? String(indexed.size) : undefined,
          webViewLink: indexed.web_view_link,
        };
        const folderId = indexed.parents[0] ?? config.rootFolderId;
        const asset = await cache.cacheSingle(client, folderId, file);
        return ok({ asset, reference_image_path: asset.media }, "Downloaded Google Drive file");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_import_url",
    "Import a public or private Google Drive file/folder URL. Private links require OAuth account view permission.",
    {
      url: z.string().describe("Google Drive file or folder URL"),
      recursive: z.boolean().optional(),
      limit: z.number().int().positive().optional(),
      ...internalAgentFields,
    },
    async ({ url, recursive, limit, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const id = parseDriveID(url);
        const file = await client.getFile(id);
        if (file.mimeType === "application/vnd.google-apps.folder") {
          if (!config.allowPublicLinkImport) await client.assertFolderInScope(id);
          const listed = await client.listFolder(id, recursive ?? true, Math.min(Math.max(limit ?? config.maxAssets, 1), config.maxAssets * 10));
          const synced = await cache.syncFolder(client, id, [file, ...listed]);
          return ok({ folder_id: id, synced }, "Imported Google Drive folder URL");
        }

        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        const inGrant = file.parents?.some((parent) => allowed.includes(parent)) ?? false;
        if (!inGrant && !config.allowPublicLinkImport) {
          throw new Error("file is outside granted folders and public link import is disabled");
        }
        const asset = await cache.cacheSingle(client, file.parents?.[0] ?? config.rootFolderId, file);
        return ok({ asset, reference_image_path: asset.media }, "Imported Google Drive file URL");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_sync_now",
    "Synchronize Google Drive now. Use changes for incremental sync or folder for a specific folder refresh.",
    {
      scope: z.enum(["changes", "folder", "root"]).optional().describe("Sync scope"),
      folder_id_or_url: z.string().optional().describe("Required for scope=folder"),
      recursive: z.boolean().optional(),
      ...internalAgentFields,
    },
    async ({ scope, folder_id_or_url }) => {
      try {
        const mode = scope ?? "changes";
        if (mode === "root") {
          return ok(await cache.syncRoot(client), "Root sync completed");
        }
        if (mode === "folder") {
          if (!folder_id_or_url) throw new Error("folder_id_or_url is required for folder sync");
          const folderId = parseDriveID(folder_id_or_url);
          return ok(await cache.syncFolder(client, folderId), "Folder sync completed");
        }
        return ok(await cache.syncChanges(client), "Changes sync completed");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  // Backward-compatible tools.
  server.tool("gdrive_list_product_folders", "Deprecated alias for gdrive_list_allowed_folders.", internalAgentFields, async (args) => {
    try {
      const folders = await cache.allowedFolders(args.goclaw_agent_id ?? "", args.goclaw_agent_key ?? "");
      return ok({ folders }, `Found ${folders.length} allowed folders`);
    } catch (e) {
      return errorResult(e);
    }
  });

  server.tool(
    "gdrive_get_product_assets",
    "Deprecated alias for gdrive_search. Searches cached assets inside granted folders.",
    {
      product: z.string().optional(),
      folder_id: z.string().optional(),
      limit: z.number().int().positive().optional(),
      ...internalAgentFields,
    },
    async ({ product, folder_id, limit, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        if (folder_id && !allowed.includes(folder_id)) {
          await cache.listFolder(folder_id, allowed, true, 1);
        }
        const assets = await cache.search(product ?? "", folder_id ? [folder_id] : allowed, Math.min(Math.max(limit ?? 12, 1), config.maxAssets));
        return ok({ product, folder_id, assets }, `Loaded ${assets.length} image assets`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_sync_product_folder",
    "Deprecated alias for gdrive_sync_now(scope=folder).",
    {
      product: z.string().optional(),
      folder_id: z.string().optional(),
      limit: z.number().int().positive().optional(),
      ...internalAgentFields,
    },
    async ({ folder_id }) => {
      try {
        const synced = folder_id ? await cache.syncFolder(client, folder_id) : await cache.syncChanges(client);
        return ok(synced, "Synced product folder");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_get_asset",
    "Deprecated alias for gdrive_download.",
    {
      file_id: z.string(),
      ...internalAgentFields,
    },
    async ({ file_id, goclaw_agent_id, goclaw_agent_key }) => {
      try {
        const allowed = cache.allowedFolderIDs(goclaw_agent_id ?? "", goclaw_agent_key ?? "");
        const indexed = await cache.assertFileAllowed(file_id, allowed);
        const file: DriveFile = {
          id: indexed.drive_file_id,
          name: indexed.name,
          mimeType: indexed.mime_type,
          parents: indexed.parents,
          modifiedTime: indexed.modified_time,
          md5Checksum: indexed.md5_checksum,
          size: indexed.size ? String(indexed.size) : undefined,
          webViewLink: indexed.web_view_link,
        };
        const asset = await cache.cacheSingle(client, indexed.parents[0] ?? config.rootFolderId, file);
        return ok({ asset }, "Downloaded Google Drive asset");
      } catch (e) {
        return errorResult(e);
      }
    },
  );
}
