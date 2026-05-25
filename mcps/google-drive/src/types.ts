export interface DriveConfig {
  clientId: string;
  clientSecret: string;
  refreshToken: string;
  rootFolderId: string;
  rootFolderName?: string;
  cacheDir: string;
  cacheTTLSeconds: number;
  maxAssets: number;
}

export interface DriveFile {
  id: string;
  name: string;
  mimeType: string;
  parents?: string[];
  modifiedTime?: string;
  size?: string;
  webViewLink?: string;
}

export interface CachedAsset {
  drive_file_id: string;
  name: string;
  mime_type: string;
  modified_time?: string;
  size?: number;
  local_path: string;
  media: string;
  role: "candidate";
  web_view_link?: string;
}

export interface SyncResult {
  changed: boolean;
  assets: CachedAsset[];
  asset_versions: Record<string, string>;
}
