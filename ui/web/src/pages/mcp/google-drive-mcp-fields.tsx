import type { UseFormReturn } from "react-hook-form";
import { ChevronRight, Folder, FolderOpen, Loader2, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useHttp } from "@/hooks/use-ws";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import type { MCPFormData } from "@/schemas/mcp.schema";
import { toast } from "@/stores/use-toast-store";
import type { MCPGoogleDriveFolder } from "@/types/mcp";

interface GoogleDriveMcpFieldsProps {
  form: UseFormReturn<MCPFormData>;
  serverId?: string;
}

export function GoogleDriveMcpFields({ form, serverId }: GoogleDriveMcpFieldsProps) {
  const { watch, setValue } = form;
  const http = useHttp();
  const { agents } = useAgents();
  const [grantAgent, setGrantAgent] = useState("");
  const [grantFolderIDs, setGrantFolderIDs] = useState<string[]>([]);
  const [manualFolders, setManualFolders] = useState("");
  const [driveFolders, setDriveFolders] = useState<MCPGoogleDriveFolder[]>([]);
  const [folderStatus, setFolderStatus] = useState<DriveFolderStatus>({ status: "idle" });
  const [foldersLoading, setFoldersLoading] = useState(false);
  const [syncStarting, setSyncStarting] = useState(false);
  const [indexStarting, setIndexStarting] = useState(false);
  const [expandedFolderIDs, setExpandedFolderIDs] = useState<Set<string>>(new Set());
  const drive = watch("googleDrive") ?? {
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
    visual_index_enabled: true,
    visual_index_provider: "",
    visual_index_model: "",
    visual_format_provider: "",
    visual_format_model: "",
    visual_index_concurrency: 1,
    visual_index_max_per_run: 100,
    visual_index_time: "",
  };

  const update = (patch: Partial<MCPFormData["googleDrive"]>) => {
    setValue("googleDrive", { ...drive, ...patch }, { shouldDirty: true });
  };

  const grants = useMemo(() => parseGrantJSON(drive.agent_folder_grants), [drive.agent_folder_grants]);
  const grantRows = useMemo(() => Object.entries(grants), [grants]);
  const folderOptions = useMemo(() => {
    const options = [...driveFolders];
    if (drive.root_folder_id && !options.some((folder) => folder.id === drive.root_folder_id)) {
      options.unshift({
        id: drive.root_folder_id,
        name: drive.root_folder_name || "Root folder",
        path: drive.root_folder_name || "Root folder",
      });
    }
    return options;
  }, [drive.root_folder_id, drive.root_folder_name, driveFolders]);
  const folderByID = useMemo(() => new Map(folderOptions.map((folder) => [folder.id, folder])), [folderOptions]);
  const agentLabelByKey = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) {
      map.set(a.agent_key, a.display_name || a.agent_key);
      map.set(a.id, a.display_name || a.agent_key);
    }
    return map;
  }, [agents]);

  const loadDriveFolders = useCallback(() => {
    if (!serverId) {
      setDriveFolders([]);
      setFolderStatus({ status: "unsaved" });
      return;
    }
    let cancelled = false;
    setFoldersLoading(true);
    http.get<DriveFolderResponse>(`/v1/mcp/servers/${serverId}/google-drive/folders`)
      .then((res) => {
        if (!cancelled) {
          setDriveFolders(res.folders ?? []);
          setFolderStatus({
            status: res.status ?? "ok",
            syncing: res.sync?.syncing,
            indexedFiles: res.sync?.indexed_files,
            downloadedFiles: res.sync?.downloaded_files,
            errors: res.sync?.errors ?? [],
            visualIndex: res.visual_index,
          });
        }
      })
      .catch(() => {
        if (!cancelled) {
          setDriveFolders([]);
          setFolderStatus({ status: "error" });
        }
      })
      .finally(() => {
        if (!cancelled) setFoldersLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [http, serverId]);

  useEffect(() => loadDriveFolders(), [loadDriveFolders]);

  useEffect(() => {
    if (drive.root_folder_id) {
      setExpandedFolderIDs((prev) => new Set(prev).add(drive.root_folder_id));
    }
  }, [drive.root_folder_id]);

  const startDriveSync = async () => {
    if (!serverId) return;
    setSyncStarting(true);
    try {
      await http.post(`/v1/mcp/servers/${serverId}/google-drive/sync`, {});
      setFolderStatus((prev) => ({ ...prev, status: "changes_sync_started", syncing: true }));
      window.setTimeout(() => loadDriveFolders(), 1500);
    } finally {
      setSyncStarting(false);
    }
  };

  const startImageIndex = async () => {
    if (!serverId) return;
    if (!drive.visual_index_provider || !drive.visual_index_model) {
      toast.error("Vision provider and model are required");
      return;
    }
    if ((drive.visual_format_provider && !drive.visual_format_model) || (!drive.visual_format_provider && drive.visual_format_model)) {
      toast.error("Format provider and model must be configured together");
      return;
    }
    setIndexStarting(true);
    setFolderStatus((prev) => ({
      ...prev,
      visualIndex: {
        indexing: true,
        indexed_images: prev.visualIndex?.indexed_images ?? 0,
        pending_images: prev.visualIndex?.pending_images ?? 0,
        failed_images: prev.visualIndex?.failed_images ?? 0,
        errors: prev.visualIndex?.errors ?? [],
      },
    }));
    try {
      await http.post(`/v1/mcp/servers/${serverId}/google-drive/index-images`, {});
      toast.success("Image indexing started");
      window.setTimeout(() => loadDriveFolders(), 1000);
      window.setTimeout(() => loadDriveFolders(), 5000);
    } catch (err) {
      toast.error("Failed to start image indexing", err instanceof Error ? err.message : String(err));
    } finally {
      setIndexStarting(false);
    }
  };

  const writeGrants = (next: Record<string, string[]>) => {
    update({ agent_folder_grants: JSON.stringify(next) });
  };

  const addOrUpdateGrant = () => {
    const key = grantAgent.trim();
    const folders = [...new Set([...grantFolderIDs, ...parseFolderIDs(manualFolders)])];
    if (!key || folders.length === 0) return;
    writeGrants({ ...grants, [key]: folders });
    setGrantAgent("");
    setGrantFolderIDs([]);
    setManualFolders("");
  };

  const removePendingFolder = (folderID: string) => {
    setGrantFolderIDs((prev) => prev.filter((id) => id !== folderID));
  };

  const togglePendingFolder = (folderID: string) => {
    setGrantFolderIDs((prev) => prev.includes(folderID) ? prev.filter((id) => id !== folderID) : [...prev, folderID]);
  };

  const toggleExpandedFolder = (folderID: string) => {
    setExpandedFolderIDs((prev) => {
      const next = new Set(prev);
      if (next.has(folderID)) next.delete(folderID);
      else next.add(folderID);
      return next;
    });
  };

  const removeGrant = (key: string) => {
    const next = { ...grants };
    delete next[key];
    writeGrants(next);
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
        <div className="grid gap-1.5 sm:col-span-1">
          <Label>Daily sync time</Label>
          <Input
            value={drive.sync_time ?? "00:00"}
            onChange={(e) => update({ sync_time: e.target.value })}
            placeholder="00:00"
            className="font-mono"
          />
        </div>
        <div className="grid gap-1.5 sm:col-span-1">
          <Label>Timezone</Label>
          <Input
            value={drive.timezone ?? "Asia/Ho_Chi_Minh"}
            onChange={(e) => update({ timezone: e.target.value })}
            placeholder="Asia/Ho_Chi_Minh"
            className="font-mono"
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
        <div className="grid gap-2 sm:col-span-3">
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4 accent-primary"
              checked={drive.visual_index_enabled ?? true}
              onChange={(e) => update({ visual_index_enabled: e.target.checked })}
            />
            <Label>Visual image indexing</Label>
          </div>
          <ProviderModelSelect
            provider={drive.visual_index_provider ?? ""}
            onProviderChange={(value) => update({ visual_index_provider: value })}
            model={drive.visual_index_model ?? ""}
            onModelChange={(value) => update({ visual_index_model: value })}
            providerLabel="Vision provider"
            modelLabel="Vision model"
            providerPlaceholder="Select provider"
            modelPlaceholder="Select or enter model"
            allowEmpty
          />
          <ProviderModelSelect
            provider={drive.visual_format_provider ?? ""}
            onProviderChange={(value) => update({ visual_format_provider: value })}
            model={drive.visual_format_model ?? ""}
            onModelChange={(value) => update({ visual_format_model: value })}
            providerLabel="Format provider"
            modelLabel="Format model"
            providerPlaceholder="Optional formatter"
            modelPlaceholder="Select or enter model"
            allowEmpty
          />
          <div className="grid gap-2 sm:grid-cols-3">
            <div className="grid gap-1.5">
              <Label>Index concurrency</Label>
              <Input
                type="number"
                min={1}
                value={drive.visual_index_concurrency ?? 1}
                onChange={(e) => update({ visual_index_concurrency: Math.max(1, Number(e.target.value) || 1) })}
              />
            </div>
            <div className="grid gap-1.5">
              <Label>Max images per run</Label>
              <Input
                type="number"
                min={0}
                value={drive.visual_index_max_per_run ?? 100}
                onChange={(e) => update({ visual_index_max_per_run: Math.max(0, Number(e.target.value) || 0) })}
              />
              <p className="text-[11px] text-muted-foreground">Use 0 to index all pending cached images.</p>
            </div>
            <div className="grid gap-1.5">
              <Label>Visual index time</Label>
              <Input
                value={drive.visual_index_time ?? ""}
                onChange={(e) => update({ visual_index_time: e.target.value })}
                placeholder="00:30"
                className="font-mono"
              />
            </div>
          </div>
        </div>
        <div className="grid gap-2 sm:col-span-3">
          <Label>Agent folder access</Label>
          <div className="grid gap-2 rounded-md border border-border p-3">
            {grantRows.length > 0 && (
              <div className="grid gap-2">
                {grantRows.map(([agentKey, folderIDs]) => (
                  <div key={agentKey} className="flex items-start justify-between gap-2 rounded-md bg-muted/30 px-3 py-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{agentLabelByKey.get(agentKey) ?? agentKey}</div>
                      <div className="mt-1 flex flex-wrap gap-1">
                        {folderIDs.map((id) => (
                          <span key={id} className="rounded bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
                            {folderByID.get(id)?.path ?? id}
                          </span>
                        ))}
                      </div>
                    </div>
                    <Button type="button" variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={() => removeGrant(agentKey)}>
                      <Trash2 className="h-3.5 w-3.5 text-destructive" />
                    </Button>
                  </div>
                ))}
              </div>
            )}

            <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <Select value={grantAgent} onValueChange={setGrantAgent}>
                <SelectTrigger>
                  <SelectValue placeholder="Select agent" />
                </SelectTrigger>
                <SelectContent>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.agent_key}>
                      <span>{a.display_name || a.agent_key}</span>
                      <span className="ml-2 text-xs text-muted-foreground">{a.agent_key}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" onClick={addOrUpdateGrant} disabled={!grantAgent || grantFolderIDs.length + parseFolderIDs(manualFolders).length === 0} className="gap-1">
                <Plus className="h-4 w-4" />
                Add
              </Button>
            </div>

            <DriveFolderPicker
              folders={folderOptions}
              selectedIDs={grantFolderIDs}
              expandedIDs={expandedFolderIDs}
              loading={foldersLoading}
              onToggleSelected={togglePendingFolder}
              onToggleExpanded={toggleExpandedFolder}
            />

            {grantFolderIDs.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {grantFolderIDs.map((id) => (
                  <button
                    key={id}
                    type="button"
                    className="rounded bg-muted px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted/80"
                    onClick={() => removePendingFolder(id)}
                    title="Click to remove"
                  >
                    {folderByID.get(id)?.path ?? id}
                  </button>
                ))}
              </div>
            )}

            <div className="flex flex-wrap items-center justify-between gap-2">
              <DriveFolderStatusLine status={folderStatus} loading={foldersLoading} folderCount={folderOptions.length} />
              {serverId && (
                <div className="flex flex-wrap gap-2">
                  <Button type="button" variant="outline" size="sm" onClick={startDriveSync} disabled={syncStarting || foldersLoading || indexStarting}>
                    {syncStarting ? "Syncing changes..." : "Sync changes"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={startImageIndex}
                    disabled={indexStarting || foldersLoading || syncStarting || !drive.visual_index_enabled}
                  >
                    {indexStarting ? "Indexing images..." : "Index images"}
                  </Button>
                </div>
              )}
            </div>
            {folderStatus.visualIndex && (
              <p className="text-xs text-muted-foreground">
                Visual index: {folderStatus.visualIndex.indexed_images} indexed, {folderStatus.visualIndex.pending_images} pending, {folderStatus.visualIndex.failed_images} failed
                {folderStatus.visualIndex.indexing ? " — indexing now" : ""}
              </p>
            )}

            <details className="group">
              <summary className="cursor-pointer text-xs text-muted-foreground">Advanced</summary>
              <Input
                value={manualFolders}
                onChange={(e) => setManualFolders(e.target.value)}
                placeholder="Extra folder IDs, comma or newline separated"
                className="mt-2 font-mono text-xs"
              />
              <Textarea
                value={drive.agent_folder_grants ?? "{}"}
                onChange={(e) => update({ agent_folder_grants: e.target.value })}
                placeholder='{"baketek-agent":["folder_id_1"],"agent_uuid":["folder_id_2"]}'
                className="mt-2 min-h-24 font-mono text-xs"
              />
            </details>
          </div>
          <p className="text-xs text-muted-foreground">Empty grants default to root access. Once at least one grant exists, unlisted agents cannot access Drive files.</p>
        </div>
      </div>
    </div>
  );
}

interface DriveFolderResponse {
  folders?: MCPGoogleDriveFolder[];
  status?: string;
  sync?: {
    syncing?: boolean;
    indexed_files?: number;
    downloaded_files?: number;
    errors?: string[];
  };
  visual_index?: VisualIndexStatus;
}

interface DriveFolderStatus {
  status: "idle" | "unsaved" | "error" | string;
  syncing?: boolean;
  indexedFiles?: number;
  downloadedFiles?: number;
  errors?: string[];
  visualIndex?: VisualIndexStatus;
}

interface VisualIndexStatus {
  indexing?: boolean;
  indexed_images?: number;
  pending_images?: number;
  failed_images?: number;
  last_indexed_at?: string;
  errors?: string[];
}

function DriveFolderStatusLine({ status, loading, folderCount }: { status: DriveFolderStatus; loading: boolean; folderCount: number }) {
  let text = "";
  if (loading) {
    text = "Loading Drive folder index...";
  } else if (status.status === "unsaved") {
    text = "Save this MCP first. Root folder can be granted now; child folders appear after initial sync.";
  } else if (status.status === "index_not_ready") {
    text = "Initial sync has not produced an index yet. Root folder is available; child folders will appear after sync.";
  } else if (status.syncing) {
    text = "Google Drive initial sync is running. Child folders will appear here automatically after the index is written.";
  } else if (status.status === "error") {
    text = "Could not load Google Drive folder index.";
  } else {
    text = `Folder index loaded: ${folderCount} folder${folderCount === 1 ? "" : "s"}`;
    if (typeof status.indexedFiles === "number") text += `, ${status.indexedFiles} files indexed`;
  }
  if (!text) return null;
  return <p className="text-xs text-muted-foreground">{text}</p>;
}

function DriveFolderPicker({
  folders,
  selectedIDs,
  expandedIDs,
  loading,
  onToggleSelected,
  onToggleExpanded,
}: {
  folders: MCPGoogleDriveFolder[];
  selectedIDs: string[];
  expandedIDs: Set<string>;
  loading: boolean;
  onToggleSelected: (folderID: string) => void;
  onToggleExpanded: (folderID: string) => void;
}) {
  const selected = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const tree = useMemo(() => buildFolderTree(folders), [folders]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-md border border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading Drive folders...
      </div>
    );
  }
  if (folders.length === 0) {
    return (
      <div className="rounded-md border border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
        No synced folders yet. Save this MCP, then start sync.
      </div>
    );
  }

  return (
    <div className="max-h-72 overflow-auto rounded-md border border-border bg-background py-1">
      {tree.map((node) => (
        <DriveFolderTreeRow
          key={node.folder.id}
          node={node}
          depth={0}
          selected={selected}
          expandedIDs={expandedIDs}
          onToggleSelected={onToggleSelected}
          onToggleExpanded={onToggleExpanded}
        />
      ))}
    </div>
  );
}

interface FolderTreeNode {
  folder: MCPGoogleDriveFolder;
  children: FolderTreeNode[];
}

function DriveFolderTreeRow({
  node,
  depth,
  selected,
  expandedIDs,
  onToggleSelected,
  onToggleExpanded,
}: {
  node: FolderTreeNode;
  depth: number;
  selected: Set<string>;
  expandedIDs: Set<string>;
  onToggleSelected: (folderID: string) => void;
  onToggleExpanded: (folderID: string) => void;
}) {
  const hasChildren = node.children.length > 0;
  const expanded = expandedIDs.has(node.folder.id);
  return (
    <div>
      <div
        className="flex items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted/50"
        style={{ paddingLeft: `${depth * 18 + 8}px` }}
      >
        <button
          type="button"
          className="flex h-5 w-5 shrink-0 items-center justify-center rounded hover:bg-muted disabled:opacity-40"
          onClick={() => onToggleExpanded(node.folder.id)}
          disabled={!hasChildren}
          title={expanded ? "Collapse folder" : "Expand folder"}
        >
          <ChevronRight className={`h-3.5 w-3.5 transition-transform ${expanded ? "rotate-90" : ""}`} />
        </button>
        {expanded ? (
          <FolderOpen className="h-4 w-4 shrink-0 text-yellow-600" />
        ) : (
          <Folder className="h-4 w-4 shrink-0 text-yellow-600" />
        )}
        <input
          type="checkbox"
          className="h-4 w-4 shrink-0 accent-primary"
          checked={selected.has(node.folder.id)}
          onChange={() => onToggleSelected(node.folder.id)}
          aria-label={`Grant ${node.folder.path}`}
        />
        <span className="truncate" title={node.folder.path}>{node.folder.name}</span>
      </div>
      {expanded && node.children.map((child) => (
        <DriveFolderTreeRow
          key={child.folder.id}
          node={child}
          depth={depth + 1}
          selected={selected}
          expandedIDs={expandedIDs}
          onToggleSelected={onToggleSelected}
          onToggleExpanded={onToggleExpanded}
        />
      ))}
    </div>
  );
}

function buildFolderTree(folders: MCPGoogleDriveFolder[]): FolderTreeNode[] {
  const byID = new Map<string, FolderTreeNode>();
  for (const folder of folders) {
    byID.set(folder.id, { folder, children: [] });
  }
  const roots: FolderTreeNode[] = [];
  for (const node of byID.values()) {
    const parent = node.folder.parent ? byID.get(node.folder.parent) : undefined;
    if (parent && parent.folder.id !== node.folder.id) parent.children.push(node);
    else roots.push(node);
  }
  const sortNodes = (nodes: FolderTreeNode[]) => {
    nodes.sort((a, b) => a.folder.name.localeCompare(b.folder.name));
    for (const node of nodes) sortNodes(node.children);
  };
  sortNodes(roots);
  return roots;
}

function parseGrantJSON(raw?: string): Record<string, string[]> {
  if (!raw?.trim()) return {};
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const out: Record<string, string[]> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (!key.trim()) continue;
      if (Array.isArray(value)) {
        const ids = value.map((v) => String(v).trim()).filter(Boolean);
        if (ids.length > 0) out[key] = ids;
      }
    }
    return out;
  } catch {
    return {};
  }
}

function parseFolderIDs(raw: string): string[] {
  return [...new Set(raw.split(/[\s,]+/).map((v) => v.trim()).filter(Boolean))];
}
