import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, CheckCircle2, XCircle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { MCPServerData, MCPServerInput } from "./hooks/use-mcp";
import { isValidSlug } from "@/lib/slug";
import { mcpFormSchema, type MCPFormData } from "@/schemas/mcp.schema";
import { McpConnectionFields } from "./mcp-connection-fields";
import { McpSettingsFields } from "./mcp-settings-fields";
import { FacebookMcpFields } from "./facebook-mcp-fields";
import { GoogleDriveMcpFields } from "./google-drive-mcp-fields";

/** Split a string into shell-like tokens, treating commas and spaces outside quotes as delimiters. */
function splitShellTokens(input: string): string[] {
  const tokens: string[] = [];
  const re = /"([^"]*)"|'([^']*)'|[^\s,]+/g;
  let m;
  while ((m = re.exec(input)) !== null) {
    tokens.push(m[1] ?? m[2] ?? m[0]);
  }
  return tokens.filter(Boolean);
}

function restoreFacebookPages(
  pages: NonNullable<NonNullable<MCPServerData["settings"]>["facebook"]>["pages"],
  headers: Record<string, string>,
): MCPFormData["facebookPages"] {
  const restored = (pages ?? []).map((page, idx) => ({
    ...page,
    watermarks: page.watermarks ?? (page.watermark ? [page.watermark] : undefined),
    access_token: headers[`x-fb-page-${idx + 1}-token`] ?? "",
    comment_schedule: page.comment_schedule ?? parseHeaderJSON(headers[`x-fb-page-${idx + 1}-comment-schedule`]),
  }));
  if (restored.length > 0) return restored;

  const legacyID = headers["x-facebook-page-id"];
  if (!legacyID) return [];
  return [{
    page_id: legacyID,
    name: headers["x-facebook-page-name"] ?? "",
    access_token: headers.Authorization?.replace(/^Bearer\s+/i, "") ?? "",
    watermark: undefined,
    watermarks: undefined,
    comment_schedule: parseHeaderJSON(headers["x-facebook-comment-schedule"]),
  }];
}

function parseHeaderJSON(raw?: string) {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

const defaultGoogleDrive: MCPFormData["googleDrive"] = {
  client_id: "",
  client_secret: "",
  refresh_token: "",
  root_folder_id: "",
  root_folder_name: "",
  cache_dir: "/app/workspace/drive-cache",
  cache_ttl_seconds: 300,
  max_assets: 50,
  agent_folder_grants: "{}",
  allow_public_link_import: true,
  sync_time: "00:00",
  timezone: "Asia/Ho_Chi_Minh",
};

function sanitizeWatermark(watermark: NonNullable<MCPFormData["facebookPages"][number]["watermark"]>) {
  const cleaned = { ...watermark };
  delete cleaned.logo_preview_url;
  if (cleaned.logo_url) delete cleaned.logo_path;
  return cleaned;
}

function stripWatermarkPreview(watermark: NonNullable<MCPFormData["facebookPages"][number]["watermark"]>) {
  const cleaned = { ...watermark };
  delete cleaned.logo_preview_url;
  return cleaned;
}

function restoreGoogleDrive(
  settings: NonNullable<MCPServerData["settings"]>["google_drive"] | undefined,
  headers: Record<string, string>,
): MCPFormData["googleDrive"] {
  return {
    ...defaultGoogleDrive,
    client_id: headers["x-gdrive-client-id"] ?? settings?.client_id ?? "",
    client_secret: headers["x-gdrive-client-secret"] ?? "",
    refresh_token: headers["x-gdrive-refresh-token"] ?? "",
    root_folder_id: headers["x-gdrive-root-folder-id"] ?? settings?.root_folder_id ?? "",
    root_folder_name: headers["x-gdrive-root-folder-name"] ?? settings?.root_folder_name ?? "",
    cache_dir: headers["x-gdrive-cache-dir"] ?? settings?.cache_dir ?? defaultGoogleDrive.cache_dir,
    cache_ttl_seconds: Number(headers["x-gdrive-cache-ttl-seconds"] ?? settings?.cache_ttl_seconds ?? defaultGoogleDrive.cache_ttl_seconds),
    max_assets: Number(headers["x-gdrive-max-assets"] ?? settings?.max_assets ?? defaultGoogleDrive.max_assets),
    agent_folder_grants: headers["x-gdrive-agent-folder-grants"] ?? settings?.agent_folder_grants ?? "{}",
    allow_public_link_import: (headers["x-gdrive-allow-public-link-import"] ?? String(settings?.allow_public_link_import ?? "true")) !== "false",
    sync_time: headers["x-gdrive-sync-time"] ?? settings?.sync_time ?? "00:00",
    timezone: headers["x-gdrive-timezone"] ?? settings?.timezone ?? "Asia/Ho_Chi_Minh",
  };
}

function stripFacebookPageForSettings(page: MCPFormData["facebookPages"][number]) {
  const { access_token: _token, watermark, watermarks, ...rest } = page;
  return {
    ...rest,
    watermark: watermark ? stripWatermarkPreview(watermark) : undefined,
    watermarks: watermarks?.map(stripWatermarkPreview),
    comment_schedule: page.comment_schedule,
  };
}

function stripGoogleDriveForSettings(drive: MCPFormData["googleDrive"]) {
  return {
    client_id: drive.client_id,
    root_folder_id: drive.root_folder_id,
    root_folder_name: drive.root_folder_name || undefined,
    cache_dir: drive.cache_dir || undefined,
    cache_ttl_seconds: drive.cache_ttl_seconds,
    max_assets: drive.max_assets,
    agent_folder_grants: drive.agent_folder_grants || "{}",
    allow_public_link_import: drive.allow_public_link_import,
    sync_time: drive.sync_time || "00:00",
    timezone: drive.timezone || "Asia/Ho_Chi_Minh",
  };
}

function buildFacebookHeaders(
  pages: MCPFormData["facebookPages"],
  existing: Record<string, string>,
): Record<string, string> {
  const headers = Object.fromEntries(
    Object.entries(existing).filter(([key]) => !/^x-fb-page-\d+-/i.test(key) && !/^x-facebook-/i.test(key)),
  );
  pages.forEach((page, idx) => {
    const n = idx + 1;
    if (page.page_id) headers[`x-fb-page-${n}-id`] = page.page_id;
    if (page.access_token) headers[`x-fb-page-${n}-token`] = page.access_token;
    if (page.name) headers[`x-fb-page-${n}-name`] = page.name;
    const watermarks = (page.watermarks?.length ? page.watermarks : page.watermark ? [page.watermark] : [])
      .filter((wm) => wm.enabled)
      .map(sanitizeWatermark);
    if (watermarks.length === 1) {
      headers[`x-fb-page-${n}-watermark`] = JSON.stringify(watermarks[0]);
    } else if (watermarks.length > 1) {
      headers[`x-fb-page-${n}-watermark`] = JSON.stringify({ enabled: true, items: watermarks });
    }
    if (page.comment_schedule) {
      headers[`x-fb-page-${n}-comment-schedule`] = JSON.stringify(page.comment_schedule);
    }
  });
  return headers;
}

function buildGoogleDriveHeaders(
  drive: MCPFormData["googleDrive"],
  existing: Record<string, string>,
): Record<string, string> {
  const headers = Object.fromEntries(
    Object.entries(existing).filter(([key]) => !/^x-gdrive-/i.test(key)),
  );
  if (drive.client_id) headers["x-gdrive-client-id"] = drive.client_id;
  if (drive.client_secret) headers["x-gdrive-client-secret"] = drive.client_secret;
  if (drive.refresh_token) headers["x-gdrive-refresh-token"] = drive.refresh_token;
  if (drive.root_folder_id) headers["x-gdrive-root-folder-id"] = drive.root_folder_id;
  if (drive.root_folder_name) headers["x-gdrive-root-folder-name"] = drive.root_folder_name;
  if (drive.cache_dir) headers["x-gdrive-cache-dir"] = drive.cache_dir;
  if (drive.cache_ttl_seconds) headers["x-gdrive-cache-ttl-seconds"] = String(drive.cache_ttl_seconds);
  if (drive.max_assets) headers["x-gdrive-max-assets"] = String(drive.max_assets);
  if (drive.agent_folder_grants) headers["x-gdrive-agent-folder-grants"] = drive.agent_folder_grants;
  headers["x-gdrive-allow-public-link-import"] = String(drive.allow_public_link_import ?? true);
  if (drive.sync_time) headers["x-gdrive-sync-time"] = drive.sync_time;
  if (drive.timezone) headers["x-gdrive-timezone"] = drive.timezone;
  return headers;
}

interface MCPFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server?: MCPServerData | null;
  onSubmit: (data: MCPServerInput) => Promise<unknown>;
  onTest: (data: {
    transport: string;
    command?: string;
    args?: string[];
    url?: string;
    headers?: Record<string, string>;
    env?: Record<string, string>;
  }) => Promise<{ success: boolean; tool_count?: number; error?: string }>;
}

export function MCPFormDialog({ open, onOpenChange, server, onSubmit, onTest }: MCPFormDialogProps) {
  const { t } = useTranslation("mcp");
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; tool_count?: number; error?: string } | null>(null);
  const [error, setError] = useState("");

  const form = useForm<MCPFormData>({
    resolver: zodResolver(mcpFormSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      displayName: "",
      transport: "stdio",
      command: "",
      args: "",
      url: "",
      headers: {},
      env: {},
      toolPrefix: "",
      timeout: 60,
      enabled: true,
      requireUserCreds: false,
      preset: "generic",
      facebookPages: [],
      googleDrive: { ...defaultGoogleDrive },
    },
  });

  const { watch, reset, handleSubmit: rhfHandleSubmit } = form;
  const transport = watch("transport");
  const command = watch("command");
  const args = watch("args");
  const url = watch("url");
  const headers = watch("headers") as Record<string, string>;
  const env = watch("env") as Record<string, string>;
  const isStdio = transport === "stdio";

  useEffect(() => {
    if (open) {
      reset({
        name: server?.name ?? "",
        displayName: server?.display_name ?? "",
        transport: (server?.transport as MCPFormData["transport"]) ?? "stdio",
        command: server?.command ?? "",
        args: Array.isArray(server?.args) ? server.args.join(", ") : "",
        url: server?.url ?? "",
        headers: server?.headers ?? {},
        env: server?.env ?? {},
        toolPrefix: server?.tool_prefix ?? "",
        timeout: server?.timeout_sec ?? 60,
        enabled: server?.enabled ?? true,
        requireUserCreds: server?.settings?.require_user_credentials ?? false,
        preset: server?.settings?.preset ?? "generic",
        facebookPages: restoreFacebookPages(server?.settings?.facebook?.pages ?? [], server?.headers ?? {}),
        googleDrive: restoreGoogleDrive(server?.settings?.google_drive, server?.headers ?? {}),
      });
      setError("");
      setTestResult(null);
    }
  }, [open, server, reset]);

  const buildConnectionData = () => {
    let parsedArgs: string[] | undefined = undefined;
    let resolvedCommand = command.trim();

    if (isStdio) {
      const cmdTokens = splitShellTokens(resolvedCommand);
      if (cmdTokens.length > 1) {
        resolvedCommand = cmdTokens[0]!;
        const extraArgs = cmdTokens.slice(1);
        const userArgs = args.trim() ? splitShellTokens(args) : [];
        parsedArgs = [...extraArgs, ...userArgs];
      } else if (args.trim()) {
        parsedArgs = splitShellTokens(args);
      }
    }

    const data = form.getValues();
    const nextHeaders = data.preset === "facebook"
      ? buildFacebookHeaders(data.facebookPages, headers)
      : data.preset === "google_drive"
        ? buildGoogleDriveHeaders(data.googleDrive, headers)
        : headers;

    return {
      transport,
      command: isStdio ? resolvedCommand : undefined,
      args: parsedArgs,
      url: !isStdio ? url.trim() : undefined,
      headers: !isStdio && Object.keys(nextHeaders).length > 0 ? nextHeaders : undefined,
      env: Object.keys(env).length > 0 ? env : undefined,
    };
  };

  const handleTest = async () => {
    if (isStdio && !command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !url.trim()) { setError(t("form.errors.urlRequired")); return; }
    setTesting(true);
    setError("");
    setTestResult(null);
    try {
      const result = await onTest(buildConnectionData());
      setTestResult(result);
    } catch (err: unknown) {
      setTestResult({ success: false, error: err instanceof Error ? err.message : t("form.errors.connectionFailed") });
    } finally {
      setTesting(false);
    }
  };

  const handleSubmit = rhfHandleSubmit(async (data) => {
    if (!isValidSlug(data.name.trim())) { setError(t("form.errors.nameSlug")); return; }
    if (isStdio && !data.command.trim()) { setError(t("form.errors.commandRequired")); return; }
    if (!isStdio && !data.url.trim()) { setError(t("form.errors.urlRequired")); return; }

    setLoading(true);
    setError("");
    try {
      await onSubmit({
        name: data.name.trim(),
        display_name: data.displayName.trim() || undefined,
        ...buildConnectionData(),
        tool_prefix: data.toolPrefix.trim() || undefined,
        timeout_sec: data.timeout,
        settings: {
          require_user_credentials: data.requireUserCreds,
          preset: data.preset,
          facebook: data.preset === "facebook" ? {
            pages: data.facebookPages.map(stripFacebookPageForSettings),
          } : undefined,
          google_drive: data.preset === "google_drive" ? stripGoogleDriveForSettings(data.googleDrive) : undefined,
        },
        enabled: data.enabled,
      });
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.errors.saveFailed", "Save failed"));
    } finally {
      setLoading(false);
    }
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{server ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <McpConnectionFields form={form} />
          {watch("preset") === "facebook" && <FacebookMcpFields form={form} serverId={server?.id} />}
          {watch("preset") === "google_drive" && <GoogleDriveMcpFields form={form} serverId={server?.id} />}
          <McpSettingsFields form={form} />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter className="flex-col sm:flex-row gap-2">
          <div className="flex items-center gap-2 mr-auto">
            <Button type="button" variant="secondary" size="sm" onClick={handleTest} disabled={loading || testing}>
              {testing
                ? <><Loader2 className="h-3.5 w-3.5 animate-spin mr-1" />{t("form.testing")}</>
                : t("form.testConnection")}
            </Button>
            {testResult && (
              <span className={`flex items-center gap-1 text-xs ${testResult.success ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"}`}>
                {testResult.success
                  ? <><CheckCircle2 className="h-3.5 w-3.5" />{t("form.toolsFound", { count: testResult.tool_count })}</>
                  : <><XCircle className="h-3.5 w-3.5" />{testResult.error}</>}
              </span>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              {t("form.cancel")}
            </Button>
            <Button onClick={handleSubmit} disabled={loading}>
              {loading ? t("form.saving") : server ? t("form.update") : t("form.create")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
