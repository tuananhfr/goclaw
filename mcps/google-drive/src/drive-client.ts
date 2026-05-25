import type { DriveConfig, DriveFile } from "./types.js";

const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

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
    };
  }

  async getFile(fileId: string, fields = "id,name,mimeType,parents,modifiedTime,size,webViewLink"): Promise<DriveFile> {
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

  async downloadFile(fileId: string): Promise<Buffer> {
    const token = await this.getAccessToken();
    const res = await fetch(`${DRIVE_BASE}/files/${encodeURIComponent(fileId)}?alt=media&supportsAllDrives=true`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error(`Google Drive download failed for ${fileId}: ${res.status} ${await res.text()}`);
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
      const q = `'${folderId.replace(/'/g, "\\'")}' in parents and trashed=false and ${qExtra}`;
      const params = new URLSearchParams({
        q,
        fields: "nextPageToken,files(id,name,mimeType,parents,modifiedTime,size,webViewLink)",
        pageSize: String(Math.min(Math.max(limit - files.length, 1), 100)),
        supportsAllDrives: "true",
        includeItemsFromAllDrives: "true",
        corpora: "allDrives",
      });
      if (pageToken) params.set("pageToken", pageToken);
      const page = await this.requestJSON<{ nextPageToken?: string; files?: DriveFile[] }>(`${DRIVE_BASE}/files?${params}`);
      files.push(...(page.files ?? []));
      pageToken = page.nextPageToken ?? "";
    } while (pageToken && files.length < limit);

    return files
      .slice(0, limit)
      .sort((a, b) => a.name.localeCompare(b.name, "vi", { sensitivity: "base" }));
  }

  private async requestJSON<T>(url: string): Promise<T> {
    const token = await this.getAccessToken();
    const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
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
    const res = await fetch("https://oauth2.googleapis.com/token", {
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
