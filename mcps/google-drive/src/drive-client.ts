import type { DriveConfig, DriveFile } from "./types.js";

const DRIVE_BASE = "https://www.googleapis.com/drive/v3";
const FETCH_TIMEOUT_MS = 120_000;

export class DriveClient {
  private accessToken = "";
  private accessTokenExpiresAt = 0;

  constructor(private readonly config: DriveConfig) {}

  rootFolderId(): string {
    return this.config.rootFolderId;
  }

  async health() {
    const root = await this.getFile(this.config.rootFolderId);
    return {
      ok: true,
      root_folder_id: root.id,
      root_folder_name: root.name,
      configured_root_folder_name: this.config.rootFolderName || undefined,
      cache_dir: this.config.cacheDir,
      cache_ttl_seconds: this.config.cacheTTLSeconds,
      max_assets: this.config.maxAssets,
      allow_public_link_import: this.config.allowPublicLinkImport,
      sync_time: this.config.syncTime,
      timezone: this.config.timezone,
    };
  }

  async getFile(fileId: string, fields = "id,name,mimeType,parents,modifiedTime,md5Checksum,size,webViewLink,trashed"): Promise<DriveFile> {
    return this.requestJSON<DriveFile>(`${DRIVE_BASE}/files/${encodeURIComponent(fileId)}?${new URLSearchParams({
      fields,
      supportsAllDrives: "true",
    })}`);
  }

  async listChildFolders(folderId: string): Promise<DriveFile[]> {
    return this.listChildren(folderId, "mimeType='application/vnd.google-apps.folder'");
  }

  async listImageFiles(folderId: string, limit: number): Promise<DriveFile[]> {
    const files = await this.listChildren(folderId, "mimeType contains 'image/'", limit);
    return files.filter((file) => file.mimeType.startsWith("image/"));
  }

  async listFolder(folderId: string, recursive: boolean, limit = Number.POSITIVE_INFINITY): Promise<DriveFile[]> {
    if (!recursive) return this.listChildren(folderId, "true", limit);
    const out: DriveFile[] = [];
    const queue = [folderId];
    const seen = new Set<string>();
    while (queue.length > 0 && out.length < limit) {
      const current = queue.shift()!;
      if (seen.has(current)) continue;
      seen.add(current);
      const remaining = Number.isFinite(limit) ? Math.max(limit - out.length, 1) : Number.POSITIVE_INFINITY;
      const children = await this.listChildren(current, "true", Math.min(1000, remaining));
      for (const child of children) {
        out.push(child);
        if (child.mimeType === "application/vnd.google-apps.folder") queue.push(child.id);
      }
    }
    return Number.isFinite(limit) ? out.slice(0, limit) : out;
  }

  async listRootRecursive(limit = Number.POSITIVE_INFINITY): Promise<DriveFile[]> {
    const root = await this.getFile(this.config.rootFolderId);
    return [root, ...(await this.listFolder(this.config.rootFolderId, true, limit))];
  }

  async getStartPageToken(): Promise<string> {
    const res = await this.requestJSON<{ startPageToken: string }>(`${DRIVE_BASE}/changes/startPageToken?${new URLSearchParams({
      supportsAllDrives: "true",
    })}`);
    return res.startPageToken;
  }

  async listChanges(pageToken: string, limit: number): Promise<{ files: DriveFile[]; newStartPageToken?: string; nextPageToken?: string }> {
    const files: DriveFile[] = [];
    let token = pageToken;
    let newStartPageToken = "";
    let nextPageToken = "";
    do {
      const params = new URLSearchParams({
        pageToken: token,
        pageSize: String(Math.min(Math.max(limit - files.length, 1), 1000)),
        fields: "nextPageToken,newStartPageToken,changes(file(id,name,mimeType,parents,modifiedTime,md5Checksum,size,webViewLink,trashed),removed,fileId)",
        supportsAllDrives: "true",
        includeItemsFromAllDrives: "true",
      });
      const page = await this.requestJSON<{ nextPageToken?: string; newStartPageToken?: string; changes?: Array<{ file?: DriveFile; fileId?: string; removed?: boolean }> }>(`${DRIVE_BASE}/changes?${params}`);
      for (const change of page.changes ?? []) {
        if (change.file) {
          files.push({ ...change.file, trashed: change.removed || change.file.trashed });
        } else if (change.fileId && change.removed) {
          files.push({ id: change.fileId, name: change.fileId, mimeType: "", trashed: true });
        }
      }
      nextPageToken = page.nextPageToken ?? "";
      newStartPageToken = page.newStartPageToken ?? newStartPageToken;
      token = nextPageToken;
    } while (nextPageToken && files.length < limit);
    return { files, newStartPageToken, nextPageToken };
  }

  async downloadFile(fileId: string): Promise<Buffer> {
    const meta = await this.getFile(fileId, "id,mimeType");
    if (meta.mimeType.startsWith("application/vnd.google-apps.")) {
      return this.exportFile(fileId, exportMime(meta.mimeType));
    }
    const token = await this.getAccessToken();
    const res = await fetchWithTimeout(`${DRIVE_BASE}/files/${encodeURIComponent(fileId)}?alt=media&supportsAllDrives=true`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error(`Google Drive download failed for ${fileId}: ${res.status} ${await res.text()}`);
    }
    return Buffer.from(await res.arrayBuffer());
  }

  async exportFile(fileId: string, mimeType: string): Promise<Buffer> {
    const token = await this.getAccessToken();
    const res = await fetchWithTimeout(`${DRIVE_BASE}/files/${encodeURIComponent(fileId)}/export?${new URLSearchParams({
      mimeType,
    })}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error(`Google Drive export failed for ${fileId}: ${res.status} ${await res.text()}`);
    }
    return Buffer.from(await res.arrayBuffer());
  }

  async downloadPublicFile(fileId: string): Promise<Buffer> {
    const res = await fetchWithTimeout(`https://drive.google.com/uc?${new URLSearchParams({ export: "download", id: fileId })}`);
    if (!res.ok) {
      throw new Error(`Google Drive public download failed for ${fileId}: ${res.status} ${await res.text()}`);
    }
    return Buffer.from(await res.arrayBuffer());
  }

  async assertFolderInScope(folderId: string): Promise<void> {
    const folder = await this.getFile(folderId, "id,name,mimeType,parents");
    if (folder.mimeType !== "application/vnd.google-apps.folder") {
      throw new Error(`Google Drive item ${folderId} is not a folder`);
    }
    if (!(await this.isInRootSubtree(folderId, true))) {
      throw new Error(`Google Drive folder ${folderId} is outside configured root folder`);
    }
  }

  async assertFileInScope(fileId: string): Promise<DriveFile> {
    const file = await this.getFile(fileId);
    const parents = file.parents ?? [];
    for (const parent of parents) {
      if (await this.isInRootSubtree(parent, true)) {
        return file;
      }
    }
    throw new Error(`Google Drive file ${fileId} is outside configured root folder`);
  }

  private async isInRootSubtree(itemId: string, includeSelf: boolean): Promise<boolean> {
    const root = this.config.rootFolderId;
    if (includeSelf && itemId === root) return true;

    const queue = [itemId];
    const seen = new Set<string>();
    for (let depth = 0; depth < 50 && queue.length > 0; depth++) {
      const current = queue.shift()!;
      if (seen.has(current)) continue;
      seen.add(current);
      if (current === root) return true;

      const meta = await this.getFile(current, "id,parents");
      for (const parent of meta.parents ?? []) {
        if (parent === root) return true;
        if (!seen.has(parent)) queue.push(parent);
      }
    }
    return false;
  }

  private async listChildren(folderId: string, qExtra: string, limit = 100): Promise<DriveFile[]> {
    const files: DriveFile[] = [];
    let pageToken = "";
    do {
      const q = `'${folderId.replace(/'/g, "\\'")}' in parents and trashed=false${qExtra && qExtra !== "true" ? ` and ${qExtra}` : ""}`;
      const params = new URLSearchParams({
        q,
        fields: "nextPageToken,files(id,name,mimeType,parents,modifiedTime,md5Checksum,size,webViewLink,trashed)",
        pageSize: String(Number.isFinite(limit) ? Math.min(Math.max(limit - files.length, 1), 100) : 100),
        supportsAllDrives: "true",
        includeItemsFromAllDrives: "true",
        corpora: "allDrives",
      });
      if (pageToken) params.set("pageToken", pageToken);
      const page = await this.requestJSON<{ nextPageToken?: string; files?: DriveFile[] }>(`${DRIVE_BASE}/files?${params}`);
      files.push(...(page.files ?? []));
      pageToken = page.nextPageToken ?? "";
    } while (pageToken && files.length < limit);

    const limited = Number.isFinite(limit) ? files.slice(0, limit) : files;
    return limited
      .sort((a, b) => a.name.localeCompare(b.name, "vi", { sensitivity: "base" }));
  }

  private async requestJSON<T>(url: string): Promise<T> {
    const token = await this.getAccessToken();
    const res = await fetchWithTimeout(url, { headers: { Authorization: `Bearer ${token}` } });
    if (!res.ok) {
      throw new Error(`Google Drive API failed: ${res.status} ${await res.text()}`);
    }
    return (await res.json()) as T;
  }

  private async getAccessToken(): Promise<string> {
    const now = Date.now();
    if (this.accessToken && now < this.accessTokenExpiresAt - 60_000) {
      return this.accessToken;
    }

    const body = new URLSearchParams({
      client_id: this.config.clientId,
      client_secret: this.config.clientSecret,
      refresh_token: this.config.refreshToken,
      grant_type: "refresh_token",
    });
    const res = await fetchWithTimeout("https://oauth2.googleapis.com/token", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body,
    });
    if (!res.ok) {
      throw new Error(`Google OAuth refresh failed: ${res.status} ${await res.text()}`);
    }
    const data = await res.json() as { access_token?: string; expires_in?: number };
    if (!data.access_token) {
      throw new Error("Google OAuth refresh did not return access_token");
    }
    this.accessToken = data.access_token;
    this.accessTokenExpiresAt = now + (data.expires_in ?? 3600) * 1000;
    return this.accessToken;
  }
}

async function fetchWithTimeout(url: string, init: RequestInit = {}): Promise<Response> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } finally {
    clearTimeout(timeout);
  }
}

function exportMime(mimeType: string): string {
  switch (mimeType) {
    case "application/vnd.google-apps.document":
      return "application/pdf";
    case "application/vnd.google-apps.spreadsheet":
      return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
    case "application/vnd.google-apps.presentation":
      return "application/pdf";
    default:
      return "application/pdf";
  }
}
