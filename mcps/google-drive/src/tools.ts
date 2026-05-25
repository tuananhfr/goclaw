import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import type { DriveAssetCache } from "./cache.js";
import type { DriveClient } from "./drive-client.js";
import type { DriveConfig, DriveFile } from "./types.js";
import { normalizeName } from "./utils.js";

function ok(data: unknown, label: string) {
  return { content: [{ type: "text" as const, text: `OK: ${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

function errorResult(err: unknown) {
  return { content: [{ type: "text" as const, text: `Error: ${err instanceof Error ? err.message : String(err)}` }], isError: true };
}

export function registerDriveTools(server: McpServer, config: DriveConfig, client: DriveClient, cache: DriveAssetCache) {
  server.tool(
    "gdrive_health",
    "Check Google Drive OAuth credentials and verify the configured root folder is readable.",
    {},
    async () => {
      try {
        return ok(await client.health(), "Google Drive connection is healthy");
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_list_product_folders",
    "List product folders directly under the configured Google Drive root folder. This never searches outside the root.",
    {},
    async () => {
      try {
        const folders = await client.listChildFolders(config.rootFolderId);
        return ok({
          root_folder_id: config.rootFolderId,
          root_folder_name: config.rootFolderName || undefined,
          count: folders.length,
          folders: folders.map(folderDTO),
        }, `Found ${folders.length} product folders`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_get_product_assets",
    "Get image assets for a product folder under the configured Google Drive root. Use this before creating or editing product images.",
    {
      product: z.string().optional().describe("Product folder name, for example 'Banh dua'"),
      folder_id: z.string().optional().describe("Specific Google Drive folder ID; must be within the configured root"),
      refresh: z.boolean().optional().describe("Refresh Drive metadata before returning assets; currently always true"),
      limit: z.number().int().positive().optional().describe("Maximum image assets to return"),
    },
    async ({ product, folder_id, limit }) => {
      try {
        const folder = await resolveProductFolder(client, config, product, folder_id);
        const boundedLimit = Math.min(Math.max(limit ?? 12, 1), config.maxAssets);
        const files = await client.listImageFiles(folder.id, boundedLimit);
        const synced = await cache.syncFolder(client, folder.id, files);
        return ok({
          product: product || folder.name,
          folder_id: folder.id,
          folder_name: folder.name,
          changed: synced.changed,
          synced_at: new Date().toISOString(),
          assets: synced.assets,
          asset_versions: synced.asset_versions,
        }, `Loaded ${synced.assets.length} image assets`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_sync_product_folder",
    "Refresh and cache image assets for one product folder under the configured root. Returns asset metadata only; it does not generate content.",
    {
      product: z.string().optional().describe("Product folder name"),
      folder_id: z.string().optional().describe("Specific Google Drive folder ID; must be within the configured root"),
      limit: z.number().int().positive().optional().describe("Maximum image assets to sync"),
    },
    async ({ product, folder_id, limit }) => {
      try {
        const folder = await resolveProductFolder(client, config, product, folder_id);
        const boundedLimit = Math.min(Math.max(limit ?? config.maxAssets, 1), config.maxAssets);
        const files = await client.listImageFiles(folder.id, boundedLimit);
        const synced = await cache.syncFolder(client, folder.id, files);
        return ok({
          product: product || folder.name,
          folder_id: folder.id,
          folder_name: folder.name,
          changed: synced.changed,
          synced_at: new Date().toISOString(),
          assets: synced.assets,
          asset_versions: synced.asset_versions,
        }, `Synced ${synced.assets.length} image assets`);
      } catch (e) {
        return errorResult(e);
      }
    },
  );

  server.tool(
    "gdrive_get_asset",
    "Download one Google Drive image file by file ID after verifying it belongs to the configured root folder subtree.",
    {
      file_id: z.string().describe("Google Drive file ID"),
    },
    async ({ file_id }) => {
      try {
        const file = await client.assertFileInScope(file_id);
        if (!file.mimeType.startsWith("image/")) {
          throw new Error(`Google Drive file ${file_id} is not an image`);
        }
        const folderId = file.parents?.[0] ?? config.rootFolderId;
        const asset = await cache.cacheSingle(client, folderId, file);
        return ok({ asset }, "Downloaded Google Drive asset");
      } catch (e) {
        return errorResult(e);
      }
    },
  );
}

async function resolveProductFolder(client: DriveClient, config: DriveConfig, product?: string, folderId?: string): Promise<DriveFile> {
  if (folderId) {
    await client.assertFolderInScope(folderId);
    return client.getFile(folderId);
  }

  if (!product || product.trim() === "") {
    return client.getFile(config.rootFolderId);
  }

  const wanted = normalizeName(product);
  const folders = await client.listChildFolders(config.rootFolderId);
  const exact = folders.filter((folder) => normalizeName(folder.name) === wanted);
  if (exact.length === 1) return exact[0]!;
  if (exact.length > 1) {
    throw new Error(`Multiple product folders match "${product}"; pass folder_id explicitly`);
  }

  const partial = folders.filter((folder) => normalizeName(folder.name).includes(wanted) || wanted.includes(normalizeName(folder.name)));
  if (partial.length === 1) return partial[0]!;
  if (partial.length > 1) {
    throw new Error(`Multiple product folders partially match "${product}"; pass folder_id explicitly`);
  }
  throw new Error(`Product folder "${product}" not found under configured root folder`);
}

function folderDTO(folder: DriveFile) {
  return {
    name: folder.name,
    folder_id: folder.id,
    modified_time: folder.modifiedTime,
    web_view_link: folder.webViewLink,
  };
}
