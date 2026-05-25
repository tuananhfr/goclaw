import { promises as fs } from "node:fs";
import path from "node:path";
import type { DriveClient } from "./drive-client.js";
import type { CachedAsset, DriveConfig, DriveFile, SyncResult } from "./types.js";
import { fileExt, parseNumber, safeSegment } from "./utils.js";

interface CacheIndex {
  files: Record<string, {
    version: string;
    local_path: string;
    name: string;
    mime_type: string;
    modified_time?: string;
    size?: number;
    web_view_link?: string;
  }>;
}

export class DriveAssetCache {
  constructor(private readonly config: DriveConfig) {}

  async syncFolder(client: DriveClient, folderId: string, files: DriveFile[]): Promise<SyncResult> {
    const dir = path.join(this.config.cacheDir, safeSegment(this.config.rootFolderId), safeSegment(folderId));
    await fs.mkdir(dir, { recursive: true });

    const indexPath = path.join(dir, "index.json");
    const previous = await this.readIndex(indexPath);
    const next: CacheIndex = { files: {} };
    const assets: CachedAsset[] = [];
    let changed = false;

    for (const file of files) {
      const version = versionKey(file);
      const ext = fileExt(file.name, file.mimeType);
      const localPath = path.join(dir, `${safeSegment(file.id)}-${safeSegment(version)}${ext}`);
      const prev = previous.files[file.id];
      const needsDownload = !prev || prev.version !== version || !(await exists(localPath));
      if (needsDownload) {
        const data = await client.downloadFile(file.id);
        await fs.writeFile(localPath, data);
        changed = true;
      }

      const asset: CachedAsset = {
        drive_file_id: file.id,
        name: file.name,
        mime_type: file.mimeType,
        modified_time: file.modifiedTime,
        size: parseNumber(file.size),
        local_path: localPath,
        media: `MEDIA:${localPath}`,
        role: "candidate",
        web_view_link: file.webViewLink,
      };
      assets.push(asset);
      next.files[file.id] = {
        version,
        local_path: localPath,
        name: file.name,
        mime_type: file.mimeType,
        modified_time: file.modifiedTime,
        size: parseNumber(file.size),
        web_view_link: file.webViewLink,
      };
    }

    for (const id of Object.keys(previous.files)) {
      if (!next.files[id]) changed = true;
    }

    await fs.writeFile(indexPath, JSON.stringify(next, null, 2));

    const assetVersions: Record<string, string> = {};
    for (const file of files) {
      assetVersions[file.id] = versionKey(file);
    }

    return { changed, assets, asset_versions: assetVersions };
  }

  async cacheSingle(client: DriveClient, folderId: string, file: DriveFile): Promise<CachedAsset> {
    const dir = path.join(this.config.cacheDir, safeSegment(this.config.rootFolderId), safeSegment(folderId));
    await fs.mkdir(dir, { recursive: true });

    const indexPath = path.join(dir, "index.json");
    const index = await this.readIndex(indexPath);
    const version = versionKey(file);
    const ext = fileExt(file.name, file.mimeType);
    const localPath = path.join(dir, `${safeSegment(file.id)}-${safeSegment(version)}${ext}`);
    const prev = index.files[file.id];
    const needsDownload = !prev || prev.version !== version || !(await exists(localPath));
    if (needsDownload) {
      const data = await client.downloadFile(file.id);
      await fs.writeFile(localPath, data);
    }

    index.files[file.id] = {
      version,
      local_path: localPath,
      name: file.name,
      mime_type: file.mimeType,
      modified_time: file.modifiedTime,
      size: parseNumber(file.size),
      web_view_link: file.webViewLink,
    };
    await fs.writeFile(indexPath, JSON.stringify(index, null, 2));

    return {
      drive_file_id: file.id,
      name: file.name,
      mime_type: file.mimeType,
      modified_time: file.modifiedTime,
      size: parseNumber(file.size),
      local_path: localPath,
      media: `MEDIA:${localPath}`,
      role: "candidate",
      web_view_link: file.webViewLink,
    };
  }

  private async readIndex(indexPath: string): Promise<CacheIndex> {
    try {
      const raw = await fs.readFile(indexPath, "utf8");
      const parsed = JSON.parse(raw) as CacheIndex;
      return { files: parsed.files ?? {} };
    } catch {
      return { files: {} };
    }
  }
}

function versionKey(file: DriveFile): string {
  return `${file.modifiedTime ?? "unknown"}:${file.size ?? "0"}`;
}

async function exists(filePath: string): Promise<boolean> {
  try {
    const st = await fs.stat(filePath);
    return st.isFile();
  } catch {
    return false;
  }
}
