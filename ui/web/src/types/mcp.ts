export interface MCPWatermarkConfig {
  enabled: boolean;
  mode: "logo" | "text";
  text?: string;
  logo_path?: string;
  logo_url?: string;
  logo_preview_url?: string;
  x_pct: number;
  y_pct: number;
  scale_pct: number;
  opacity: number;
}

export interface MCPFacebookPageConfig {
  page_id: string;
  name?: string;
  watermark?: MCPWatermarkConfig;
  watermarks?: MCPWatermarkConfig[];
  comment_schedule?: {
    enabled: boolean;
    comment_count: number;
    window_ms: number;
    min_gap_ms: number;
    random_order: boolean;
  };
}

export interface MCPSettings {
  require_user_credentials?: boolean;
  preset?: "generic" | "facebook" | "google_drive";
  facebook?: {
    pages?: MCPFacebookPageConfig[];
  };
  google_drive?: {
    client_id?: string;
    root_folder_id?: string;
    root_folder_name?: string;
    cache_dir?: string;
    cache_ttl_seconds?: number;
    max_assets?: number;
    agent_folder_grants?: string;
    allow_public_link_import?: boolean;
    sync_time?: string;
    timezone?: string;
    visual_index_enabled?: boolean;
    visual_index_provider?: string;
    visual_index_model?: string;
    visual_index_concurrency?: number;
    visual_index_max_per_run?: number;
    visual_index_time?: string;
  };
}

export interface MCPServerData {
  id: string;
  name: string;
  display_name: string;
  transport: "stdio" | "sse" | "streamable-http";
  command: string;
  args: string[] | null;
  url: string;
  headers: Record<string, string> | null;
  env: Record<string, string> | null;
  tool_prefix: string;
  timeout_sec: number;
  settings?: MCPSettings;
  enabled: boolean;
  created_by: string;
  agent_count?: number;
  created_at: string;
  updated_at: string;
}

export interface MCPServerInput {
  name: string;
  display_name?: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  headers?: Record<string, string>;
  env?: Record<string, string>;
  tool_prefix?: string;
  timeout_sec?: number;
  settings?: MCPSettings;
  enabled?: boolean;
}

export interface MCPToolInfo {
  name: string;
  description?: string;
}

export interface MCPGoogleDriveFolder {
  id: string;
  name: string;
  path: string;
  parent?: string;
}

export interface MCPAgentGrant {
  id: string;
  server_id: string;
  agent_id: string;
  enabled: boolean;
  tool_allow: string[] | null;
  tool_deny: string[] | null;
  granted_by: string;
  created_at: string;
}

export interface MCPUserCredentialStatus {
  has_credentials: boolean;
  has_api_key: boolean;
  has_headers: boolean;
  has_env: boolean;
}

export interface MCPUserCredentialInput {
  api_key?: string;
  headers?: Record<string, string>;
  env?: Record<string, string>;
}
