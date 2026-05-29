export interface DriveConfig {
  clientId: string;
  clientSecret: string;
  refreshToken: string;
  rootFolderId: string;
  rootFolderName?: string;
  cacheDir: string;
  cacheTTLSeconds: number;
  maxAssets: number;
  agentFolderGrants: Record<string, string[]>;
  allowPublicLinkImport: boolean;
  syncTime: string;
  timezone: string;
}

export interface DriveFile {
  id: string;
  name: string;
  mimeType: string;
  parents?: string[];
  modifiedTime?: string;
  md5Checksum?: string;
  size?: string;
  webViewLink?: string;
  trashed?: boolean;
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
  indexed_files?: number;
  downloaded_files?: number;
  trashed_files?: number;
  errors?: string[];
}

export interface IndexedDriveFile {
  drive_file_id: string;
  name: string;
  mime_type: string;
  parents: string[];
  modified_time?: string;
  md5_checksum?: string;
  size?: number;
  web_view_link?: string;
  trashed: boolean;
  local_path?: string;
  media?: string;
  version: string;
  synced_at: string;
}

export interface GlobalDriveIndex {
  root_folder_id: string;
  last_sync_at?: string;
  last_start_page_token?: string;
  files: Record<string, IndexedDriveFile>;
  folders: Record<string, {
    drive_file_id: string;
    name: string;
    parents: string[];
    modified_time?: string;
    web_view_link?: string;
    trashed: boolean;
  }>;
  public_imports: Record<string, IndexedDriveFile>;
  status: {
    syncing: boolean;
    indexed_files: number;
    downloaded_files: number;
    trashed_files: number;
    errors: string[];
  };
}
