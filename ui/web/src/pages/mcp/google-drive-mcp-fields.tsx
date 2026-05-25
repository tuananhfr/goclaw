import type { UseFormReturn } from "react-hook-form";
import { FolderOpen } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { MCPFormData } from "@/schemas/mcp.schema";

interface GoogleDriveMcpFieldsProps {
  form: UseFormReturn<MCPFormData>;
}

export function GoogleDriveMcpFields({ form }: GoogleDriveMcpFieldsProps) {
  const { watch, setValue } = form;
  const drive = watch("googleDrive") ?? {
    client_id: "",
    client_secret: "",
    refresh_token: "",
    root_folder_id: "",
    root_folder_name: "",
    cache_dir: "/app/workspace/drive-cache",
    cache_ttl_seconds: 300,
    max_assets: 50,
  };

  const update = (patch: Partial<MCPFormData["googleDrive"]>) => {
    setValue("googleDrive", { ...drive, ...patch }, { shouldDirty: true });
  };

  return (
    <div className="grid gap-3 rounded-md border border-border p-3">
      <div className="flex items-center gap-2">
        <FolderOpen className="h-4 w-4 text-muted-foreground" />
        <div>
          <Label>Google Drive folder scope</Label>
          <p className="text-xs text-muted-foreground">This MCP can only expose images under the configured root folder.</p>
        </div>
      </div>

      <div className="grid gap-2">
        <Label>OAuth client</Label>
        <Input
          value={drive.client_id}
          onChange={(e) => update({ client_id: e.target.value })}
          placeholder="Google Drive Client ID"
          className="font-mono"
        />
        <Input
          type="password"
          value={drive.client_secret}
          onChange={(e) => update({ client_secret: e.target.value })}
          placeholder="Google Drive Client Secret"
          className="font-mono"
        />
        <Input
          type="password"
          value={drive.refresh_token}
          onChange={(e) => update({ refresh_token: e.target.value })}
          placeholder="Google Drive Refresh Token"
          className="font-mono"
        />
      </div>

      <div className="grid gap-2">
        <Label>Root folder</Label>
        <Input
          value={drive.root_folder_id}
          onChange={(e) => update({ root_folder_id: e.target.value })}
          placeholder="Google Drive root folder ID"
          className="font-mono"
        />
        <Input
          value={drive.root_folder_name ?? ""}
          onChange={(e) => update({ root_folder_name: e.target.value })}
          placeholder="Root folder name (optional)"
        />
      </div>

      <div className="grid gap-2 sm:grid-cols-3">
        <div className="grid gap-1.5 sm:col-span-1">
          <Label>Cache TTL seconds</Label>
          <Input
            type="number"
            min={1}
            value={drive.cache_ttl_seconds}
            onChange={(e) => update({ cache_ttl_seconds: Math.max(1, Number(e.target.value) || 1) })}
          />
        </div>
        <div className="grid gap-1.5 sm:col-span-1">
          <Label>Max assets</Label>
          <Input
            type="number"
            min={1}
            value={drive.max_assets}
            onChange={(e) => update({ max_assets: Math.max(1, Number(e.target.value) || 1) })}
          />
        </div>
        <div className="grid gap-1.5 sm:col-span-3">
          <Label>Cache directory</Label>
          <Input
            value={drive.cache_dir ?? ""}
            onChange={(e) => update({ cache_dir: e.target.value })}
            placeholder="/app/workspace/drive-cache"
            className="font-mono"
          />
        </div>
      </div>
    </div>
  );
}
