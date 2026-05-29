import { promises as fs } from "node:fs";
import path from "node:path";
import type { DriveClient } from "./drive-client.js";
import type { CachedAsset, DriveConfig, DriveFile, GlobalDriveIndex, IndexedDriveFile, SyncResult } from "./types.js";
import { fileExt, normalizeName, parseNumber, safeSegment } from "./utils.js";

export class DriveAssetCache {
  private readonly rootDir: string;
  private readonly indexPath: string;
  private syncPromise: Promise<SyncResult> | null = null;

  constructor(private readonly config: DriveConfig) {
    this.rootDir = path.join(this.config.cacheDir, safeSegment(this.config.rootFolderId));
    this.indexPath = path.join(this.rootDir, "index.json");
  }

  async ensureInitialSync(client: DriveClient): Promise<SyncResult> {
    const index = await this.readGlobalIndex();
    if (index.last_sync_at && Object.keys(index.files).length + Object.keys(index.folders).length > 0) {
      return this.statusResult(index);
    }
    return this.syncRoot(client);
  }

  async syncRoot(client: DriveClient): Promise<SyncResult> {
    return this.withSync(async () => {
      const files = await client.listRootRecursive(Number.POSITIVE_INFINITY);
      const index = await this.readGlobalIndex();
      index.status = { syncing: true, indexed_files: 0, downloaded_files: 0, trashed_files: 0, errors: [] };
      await this.writeGlobalIndex(index);
      try {
        const result = await this.applyFiles(client, files, index);
        index.last_start_page_token = await client.getStartPageToken().catch(() => index.last_start_page_token);
        index.last_sync_at = new Date().toISOString();
        index.status.syncing = false;
        await this.writeGlobalIndex(index);
        return result;
      } catch (err) {
        index.status.syncing = false;
        index.status.errors = [err instanceof Error ? err.message : String(err)];
        await this.writeGlobalIndex(index);
        throw err;
      }
    });
  }

  async syncChanges(client: DriveClient): Promise<SyncResult> {
    return this.withSync(async () => {
      const index = await this.readGlobalIndex();
      if (!index.last_start_page_token) {
        return this.syncRoot(client);
      }
      index.status = { syncing: true, indexed_files: 0, downloaded_files: 0, trashed_files: 0, errors: [] };
      await this.writeGlobalIndex(index);
      try {
        let result: SyncResult = { changed: false, assets: [], asset_versions: {}, indexed_files: Object.keys(index.files).length, downloaded_files: 0, trashed_files: 0, errors: [] };
        const missing = Object.values(index.files)
          .filter((file) => !file.trashed && !file.local_path)
          .sort((a, b) => {
            const ai = a.mime_type.startsWith("image/") ? 0 : 1;
            const bi = b.mime_type.startsWith("image/") ? 0 : 1;
            if (ai !== bi) return ai - bi;
            return (a.size ?? 0) - (b.size ?? 0);
          })
          .slice(0, this.config.maxAssets * 20)
          .map(indexedFileToDriveFile);
        if (missing.length > 0) {
          result = await this.applyFiles(client, missing, index);
          index.status = {
            syncing: true,
            indexed_files: Object.keys(index.files).length,
            downloaded_files: result.downloaded_files ?? 0,
            trashed_files: result.trashed_files ?? 0,
            errors: result.errors ?? [],
          };
          await this.writeGlobalIndex(index);
        }
        const changes = await client.listChanges(index.last_start_page_token, this.config.maxAssets * 20);
        const inScope = changes.files.filter((file) => file.trashed || this.fileTouchesKnownScope(file, index));
        const changed = await this.applyFiles(client, inScope, index);
        result = {
          changed: result.changed || changed.changed,
          assets: [...result.assets, ...changed.assets],
          asset_versions: { ...result.asset_versions, ...changed.asset_versions },
          indexed_files: changed.indexed_files,
          downloaded_files: (result.downloaded_files ?? 0) + (changed.downloaded_files ?? 0),
          trashed_files: (result.trashed_files ?? 0) + (changed.trashed_files ?? 0),
          errors: [...(result.errors ?? []), ...(changed.errors ?? [])],
        };
        if (changes.newStartPageToken) index.last_start_page_token = changes.newStartPageToken;
        index.last_sync_at = new Date().toISOString();
        index.status.syncing = false;
        await this.writeGlobalIndex(index);
        return result;
      } catch (err) {
        index.status.syncing = false;
        index.status.errors = [err instanceof Error ? err.message : String(err)];
        await this.writeGlobalIndex(index);
        throw err;
      }
    });
  }

  async syncFolder(client: DriveClient, folderId: string, files?: DriveFile[]): Promise<SyncResult> {
    return this.withSync(async () => {
      const index = await this.readGlobalIndex();
      const listed = files ?? await client.listFolder(folderId, true, this.config.maxAssets * 10);
      const folder = await client.getFile(folderId).catch(() => undefined);
      const all = folder ? [folder, ...listed] : listed;
      return this.applyFiles(client, all, index);
    });
  }

  async cacheSingle(client: DriveClient, folderId: string, file: DriveFile, publicDownload = false): Promise<CachedAsset> {
    const index = await this.readGlobalIndex();
    const entry = await this.cacheFile(client, file, publicDownload);
    index.files[file.id] = entry;
    await this.writeGlobalIndex(index);
    return assetFromEntry(entry);
  }

  async search(query: string, allowedFolderIds: string[], limit: number): Promise<CachedAsset[]> {
    const q = normalizeName(query);
    const index = await this.readGlobalIndex();
    const assets = Object.values(index.files)
      .filter((file) => !file.trashed && this.entryAllowed(file, allowedFolderIds, index))
      .filter((file) => q === "" || normalizeName(file.name).includes(q))
      .sort((a, b) => (b.modified_time ?? "").localeCompare(a.modified_time ?? ""))
      .slice(0, limit)
      .map(assetFromEntry);
    return assets;
  }

  async listFolder(folderId: string, allowedFolderIds: string[], recursive: boolean, limit: number): Promise<Array<IndexedDriveFile | GlobalDriveIndex["folders"][string]>> {
    const index = await this.readGlobalIndex();
    if (!this.folderAllowed(folderId, allowedFolderIds, index)) {
      throw new Error(`folder ${folderId} is not granted to this agent`);
    }
    const items = [
      ...Object.values(index.folders),
      ...Object.values(index.files),
    ].filter((item) => !item.trashed && this.itemUnderFolder(item, folderId, recursive, index));
    return items.slice(0, limit);
  }

  async getFile(fileId: string): Promise<IndexedDriveFile | undefined> {
    const index = await this.readGlobalIndex();
    return index.files[fileId] ?? index.public_imports[fileId];
  }

  async allowedFolders(agentID: string, agentKey: string): Promise<GlobalDriveIndex["folders"][string][]> {
    const index = await this.readGlobalIndex();
    const ids = this.allowedFolderIDs(agentID, agentKey);
    return ids.map((id) => index.folders[id]).filter(Boolean);
  }

  allowedFolderIDs(agentID: string, agentKey: string): string[] {
    const grants = this.config.agentFolderGrants;
    const grantKeys = Object.keys(grants);
    const ids = [
      ...(agentKey ? grants[agentKey] ?? [] : []),
      ...(agentID ? grants[agentID] ?? [] : []),
      ...(grants["*"] ?? []),
    ];
    if (ids.length > 0) return Array.from(new Set(ids));
    return grantKeys.length === 0 ? [this.config.rootFolderId] : [];
  }

  async assertFileAllowed(fileId: string, allowedFolderIds: string[]): Promise<IndexedDriveFile> {
    const index = await this.readGlobalIndex();
    const file = index.files[fileId] ?? index.public_imports[fileId];
    if (!file || file.trashed) throw new Error(`file ${fileId} was not found in the Google Drive index`);
    if (!index.public_imports[fileId] && !this.entryAllowed(file, allowedFolderIds, index)) {
      throw new Error(`file ${fileId} is outside folders granted to this agent`);
    }
    return file;
  }

  async status() {
    return this.statusResult(await this.readGlobalIndex());
  }

  private async withSync(fn: () => Promise<SyncResult>): Promise<SyncResult> {
    if (this.syncPromise) return this.syncPromise;
    this.syncPromise = fn().finally(() => {
      this.syncPromise = null;
    });
    return this.syncPromise;
  }

  private async applyFiles(client: DriveClient, files: DriveFile[], index: GlobalDriveIndex): Promise<SyncResult> {
    let changed = false;
    let downloaded = 0;
    let trashed = 0;
    const errors: string[] = [];
    const assets: CachedAsset[] = [];
    const assetVersions: Record<string, string> = {};

    await fs.mkdir(this.rootDir, { recursive: true });
    for (const file of files) {
      if (file.trashed) continue;
      if (file.mimeType !== "application/vnd.google-apps.folder") continue;
      index.folders[file.id] = {
        drive_file_id: file.id,
        name: file.name,
        parents: file.parents ?? [],
        modified_time: file.modifiedTime,
        web_view_link: file.webViewLink,
        trashed: false,
      };
      changed = true;
    }
    for (const file of files) {
      if (file.trashed || file.mimeType === "application/vnd.google-apps.folder") continue;
      const previous = index.files[file.id];
      index.files[file.id] = {
        drive_file_id: file.id,
        name: file.name,
        mime_type: file.mimeType,
        parents: file.parents ?? [],
        modified_time: file.modifiedTime,
        md5_checksum: file.md5Checksum,
        size: parseNumber(file.size),
        web_view_link: file.webViewLink,
        trashed: false,
        local_path: previous?.version === versionKey(file) ? previous.local_path : undefined,
        media: previous?.version === versionKey(file) ? previous.media : undefined,
        version: versionKey(file),
        synced_at: new Date().toISOString(),
      };
      changed = true;
    }
    if (changed) {
      index.last_sync_at = new Date().toISOString();
      index.status = {
        ...index.status,
        syncing: true,
        indexed_files: Object.keys(index.files).length,
        downloaded_files: downloaded,
        trashed_files: trashed,
        errors,
      };
      await this.writeGlobalIndex(index);
    }

    for (const file of files) {
      try {
        if (file.trashed) {
          if (index.files[file.id]) index.files[file.id].trashed = true;
          if (index.folders[file.id]) index.folders[file.id].trashed = true;
          trashed++;
          changed = true;
          continue;
        }
        if (file.mimeType === "application/vnd.google-apps.folder") {
          continue;
        }
        const before = index.files[file.id];
        const entry = await this.cacheFile(client, file, false);
        index.files[file.id] = entry;
        assets.push(assetFromEntry(entry));
        assetVersions[file.id] = entry.version;
        if (!before?.local_path || before.version !== entry.version) downloaded++;
        changed = changed || !before?.local_path || before.version !== entry.version;
        index.status = {
          syncing: true,
          indexed_files: Object.keys(index.files).length,
          downloaded_files: downloaded,
          trashed_files: trashed,
          errors,
        };
        await this.writeGlobalIndex(index);
      } catch (err) {
        errors.push(err instanceof Error ? err.message : String(err));
      }
    }

    index.last_sync_at = new Date().toISOString();
    index.status = {
      syncing: false,
      indexed_files: Object.keys(index.files).length,
      downloaded_files: downloaded,
      trashed_files: trashed,
      errors,
    };
    await this.writeGlobalIndex(index);
    return { changed, assets, asset_versions: assetVersions, indexed_files: Object.keys(index.files).length, downloaded_files: downloaded, trashed_files: trashed, errors };
  }

  private async cacheFile(client: DriveClient, file: DriveFile, publicDownload: boolean): Promise<IndexedDriveFile> {
    const version = versionKey(file);
    const ext = fileExt(file.name, file.mimeType);
    const dir = path.join(this.rootDir, safeSegment(file.id));
    await fs.mkdir(dir, { recursive: true });
    const localPath = path.join(dir, `${safeSegment(file.name).slice(0, 120)}-${safeSegment(version).slice(0, 40)}${ext}`);
    if (!(await exists(localPath))) {
      const data = publicDownload ? await client.downloadPublicFile(file.id) : await client.downloadFile(file.id);
      await fs.writeFile(localPath, data);
    }
    return {
      drive_file_id: file.id,
      name: file.name,
      mime_type: file.mimeType,
      parents: file.parents ?? [],
      modified_time: file.modifiedTime,
      md5_checksum: file.md5Checksum,
      size: parseNumber(file.size),
      web_view_link: file.webViewLink,
      trashed: false,
      local_path: localPath,
      media: `MEDIA:${localPath}`,
      version,
      synced_at: new Date().toISOString(),
    };
  }

  private entryAllowed(file: IndexedDriveFile, allowedFolderIds: string[], index: GlobalDriveIndex): boolean {
    return file.parents.some((parent) => this.folderAllowed(parent, allowedFolderIds, index));
  }

  private folderAllowed(folderId: string, allowedFolderIds: string[], index: GlobalDriveIndex): boolean {
    if (allowedFolderIds.includes(folderId)) return true;
    const queue = [folderId];
    const seen = new Set<string>();
    while (queue.length > 0) {
      const current = queue.shift()!;
      if (seen.has(current)) continue;
      seen.add(current);
      if (allowedFolderIds.includes(current)) return true;
      const folder = index.folders[current];
      for (const parent of folder?.parents ?? []) queue.push(parent);
    }
    return false;
  }

  private itemUnderFolder(item: { parents: string[] }, folderId: string, recursive: boolean, index: GlobalDriveIndex): boolean {
    if (item.parents.includes(folderId)) return true;
    if (!recursive) return false;
    return item.parents.some((parent) => this.folderAllowed(parent, [folderId], index));
  }

  private fileTouchesKnownScope(file: DriveFile, index: GlobalDriveIndex): boolean {
    if (file.id === this.config.rootFolderId) return true;
    for (const parent of file.parents ?? []) {
      if (parent === this.config.rootFolderId || index.folders[parent] || index.files[parent]) return true;
    }
    return Boolean(index.files[file.id] || index.folders[file.id]);
  }

  private statusResult(index: GlobalDriveIndex): SyncResult {
    return {
      changed: false,
      assets: Object.values(index.files).filter((f) => !f.trashed).slice(0, this.config.maxAssets).map(assetFromEntry),
      asset_versions: Object.fromEntries(Object.values(index.files).map((f) => [f.drive_file_id, f.version])),
      indexed_files: Object.keys(index.files).length,
      downloaded_files: index.status.downloaded_files,
      trashed_files: index.status.trashed_files,
      errors: index.status.errors,
    };
  }

  private async readGlobalIndex(): Promise<GlobalDriveIndex> {
    try {
      const raw = await fs.readFile(this.indexPath, "utf8");
      const parsed = JSON.parse(raw) as GlobalDriveIndex;
      return {
        root_folder_id: parsed.root_folder_id || this.config.rootFolderId,
        last_sync_at: parsed.last_sync_at,
        last_start_page_token: parsed.last_start_page_token,
        files: parsed.files ?? {},
        folders: parsed.folders ?? {},
        public_imports: parsed.public_imports ?? {},
        status: parsed.status ?? { syncing: false, indexed_files: 0, downloaded_files: 0, trashed_files: 0, errors: [] },
      };
    } catch {
      return {
        root_folder_id: this.config.rootFolderId,
        files: {},
        folders: {},
        public_imports: {},
        status: { syncing: false, indexed_files: 0, downloaded_files: 0, trashed_files: 0, errors: [] },
      };
    }
  }

  private async writeGlobalIndex(index: GlobalDriveIndex): Promise<void> {
    await fs.mkdir(this.rootDir, { recursive: true });
    await fs.writeFile(this.indexPath, JSON.stringify(index, null, 2));
  }
}

function assetFromEntry(entry: IndexedDriveFile): CachedAsset {
  return {
    drive_file_id: entry.drive_file_id,
    name: entry.name,
    mime_type: entry.mime_type,
    modified_time: entry.modified_time,
    size: entry.size,
    local_path: entry.local_path ?? "",
    media: entry.media ?? "",
    role: "candidate",
    web_view_link: entry.web_view_link,
  };
}

function versionKey(file: DriveFile): string {
  return file.md5Checksum || `${file.modifiedTime ?? "unknown"}:${file.size ?? "0"}`;
}

function indexedFileToDriveFile(file: IndexedDriveFile): DriveFile {
  return {
    id: file.drive_file_id,
    name: file.name,
    mimeType: file.mime_type,
    parents: file.parents,
    modifiedTime: file.modified_time,
    md5Checksum: file.md5_checksum,
    size: file.size ? String(file.size) : undefined,
    webViewLink: file.web_view_link,
    trashed: file.trashed,
  };
}

async function exists(filePath: string): Promise<boolean> {
  try {
    const st = await fs.stat(filePath);
    return st.isFile();
  } catch {
    return false;
  }
}
