import { Fragment, useState, useEffect, lazy, Suspense, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Zap, RefreshCw, Upload, ScanSearch, FolderPlus, FolderInput, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { cn } from "@/lib/utils";
import { useSkills, type SkillInfo } from "./hooks/use-skills";
import { SkillDetailDialog } from "./skill-detail-dialog";
import { SkillEditDialog } from "./skill-edit-dialog";

const SkillUploadDialog = lazy(() =>
  import("./skill-upload-dialog").then((m) => ({ default: m.SkillUploadDialog }))
);
import { MissingDepsPanel } from "./missing-deps-panel";
import { SkillTableRow } from "./skill-table-row";
import { useRuntimes } from "./hooks/use-runtimes";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";
import { useTenants } from "@/hooks/use-tenants";

const MASTER_TENANT_ID = "0193a5b0-7000-7000-8000-000000000001";

type Tab = "core" | "custom";

export function SkillsPage() {
  const { t } = useTranslation("skills");
  const {
    skills, loading, refresh, getSkill, uploadSkill, updateSkill, bulkUpdateSkills, deleteSkill,
    getSkillVersions, getSkillFiles, getSkillFileContent, updateSkillFileContent, rescanDeps, installSingleDep, toggleSkill,
    setTenantConfig, deleteTenantConfig,
  } = useSkills();
  const { runtimes } = useRuntimes();
  const { currentTenantId } = useTenants();
  const hasTenantScope = !!currentTenantId && currentTenantId !== MASTER_TENANT_ID;
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && skills.length === 0);
  const [tab, setTab] = useState<Tab>("core");
  const [search, setSearch] = useState("");
  const [selectedSkill, setSelectedSkill] = useState<(SkillInfo & { content: string }) | null>(null);
  const [detailInitialTab, setDetailInitialTab] = useState<"content" | "files">("content");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<SkillInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [rescanning, setRescanning] = useState(false);
  const [toggling, setToggling] = useState<string | null>(null);
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  const [folderDialogOpen, setFolderDialogOpen] = useState(false);
  const [folderDialogMode, setFolderDialogMode] = useState<"create" | "move">("move");
  const [folderInput, setFolderInput] = useState("");
  const [folderSaving, setFolderSaving] = useState(false);

  const coreSkills = useMemo(
    () => skills.filter((s: SkillInfo) => s.is_system),
    [skills],
  );
  const customSkills = useMemo(
    () => skills.filter((s: SkillInfo) => !s.is_system),
    [skills],
  );
  const tabSkills = useMemo(
    () => (tab === "core" ? coreSkills : customSkills),
    [tab, coreSkills, customSkills],
  );
  const allMissing = [...new Set(tabSkills.flatMap((s: SkillInfo) => s.missing_deps ?? []))];
  const filtered = tabSkills.filter(
    (s: SkillInfo) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()) ||
      (s.folder ?? "").toLowerCase().includes(search.toLowerCase()),
  );
  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);
  const columnCount = tab === "custom" ? 7 : 4;
  const existingFolderOptions = useMemo(
    () => [...new Set(customSkills.map((skill) => skill.folder?.trim()).filter((folder): folder is string => !!folder))].sort(),
    [customSkills],
  );
  const selectedCustomSkills = useMemo(
    () => customSkills.filter((skill) => skill.id && selectedSkillIds.includes(skill.id)),
    [customSkills, selectedSkillIds],
  );
  const selectablePageItems = useMemo(
    () => pageItems.filter((skill): skill is SkillInfo & { id: string } => tab === "custom" && !!skill.id),
    [pageItems, tab],
  );
  const allPageSelected = selectablePageItems.length > 0 && selectablePageItems.every((skill) => selectedSkillIds.includes(skill.id));

  useEffect(() => { resetPage(); }, [search, tab, resetPage]);
  useEffect(() => {
    const validIds = new Set(customSkills.map((skill) => skill.id).filter((id): id is string => !!id));
    setSelectedSkillIds((current) => {
      const next = current.filter((id) => validIds.has(id));
      if (next.length === current.length && next.every((id, index) => id === current[index])) {
        return current;
      }
      return next;
    });
  }, [customSkills]);

  const handleViewSkill = async (name: string, initialTab: "content" | "files" = "content") => {
    setDetailInitialTab(initialTab);
    const detail = await getSkill(name);
    if (detail) setSelectedSkill(detail);
  };

  const handleCycleVisibility = async (skill: SkillInfo) => {
    if (!skill.id) return;
    const order = ["private", "internal", "public"] as const;
    const idx = order.indexOf(skill.visibility as typeof order[number]);
    await updateSkill(skill.id, { visibility: order[(idx + 1) % order.length] });
  };

  const handleDelete = async () => {
    if (!deleteTarget?.id) return;
    setDeleteLoading(true);
    try { await deleteSkill(deleteTarget.id); setDeleteTarget(null); refresh(); }
    finally { setDeleteLoading(false); }
  };

  const handleRescanDeps = async () => {
    setRescanning(true);
    try { await rescanDeps(); } finally { setRescanning(false); }
  };

  const handleToggle = async (skill: SkillInfo, enabled: boolean) => {
    if (!skill.id) return;
    setToggling(skill.id);
    try { await toggleSkill(skill.id, enabled); } finally { setToggling(null); }
  };

  const handleSetTenantConfig = async (id: string, enabled: boolean) => {
    setToggling(id);
    try { await setTenantConfig(id, enabled); } finally { setToggling(null); }
  };

  const handleDeleteTenantConfig = async (id: string) => {
    setToggling(id);
    try { await deleteTenantConfig(id); } finally { setToggling(null); }
  };

  const toggleSkillSelection = (skill: SkillInfo, checked: boolean) => {
    const skillId = skill.id;
    if (!skillId) return;
    setSelectedSkillIds((current) => {
      if (checked) {
        return current.includes(skillId) ? current : [...current, skillId];
      }
      return current.filter((id) => id !== skillId);
    });
  };

  const toggleSelectPage = (checked: boolean) => {
    const pageIds = selectablePageItems.map((skill) => skill.id);
    setSelectedSkillIds((current) => {
      if (checked) {
        return [...new Set([...current, ...pageIds])];
      }
      return current.filter((id) => !pageIds.includes(id));
    });
  };

  const openFolderDialog = (mode: "create" | "move") => {
    setFolderDialogMode(mode);
    setFolderInput("");
    setFolderDialogOpen(true);
  };

  const handleApplyFolder = async () => {
    const nextFolder = folderInput.trim();
    if (!nextFolder || selectedCustomSkills.length === 0) return;
    setFolderSaving(true);
    try {
      await bulkUpdateSkills(selectedCustomSkills.map((skill) => ({
        id: skill.id!,
        changes: { folder: nextFolder },
      })));
      setSelectedSkillIds([]);
      setFolderDialogOpen(false);
      setFolderInput("");
    } finally {
      setFolderSaving(false);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            {tab === "custom" && (
              <>
                <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
                  <Upload className="h-3.5 w-3.5" /> {t("upload.button")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => openFolderDialog("create")}
                  disabled={selectedCustomSkills.length === 0}
                  className="gap-1"
                  title={t("folder.createHint", { defaultValue: "Select skills first, then create a folder for them." })}
                >
                  <FolderPlus className="h-3.5 w-3.5" /> {t("folder.createButton", { defaultValue: "Create Folder" })}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => openFolderDialog("move")}
                  disabled={selectedCustomSkills.length === 0}
                  className="gap-1"
                >
                  <FolderInput className="h-3.5 w-3.5" /> {t("folder.moveButton", { defaultValue: "Move Selected" })}
                </Button>
              </>
            )}
            <Button variant="outline" size="sm" onClick={handleRescanDeps} disabled={rescanning} className="gap-1">
              <ScanSearch className="h-3.5 w-3.5" /> {t("deps.rescan")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("refresh", { ns: "common" })}
            </Button>
          </div>
        }
      />

      <div className="flex gap-1 border-b mt-4">
        {(["core", "custom"] as Tab[]).map((tabKey) => (
          <button
            key={tabKey}
            type="button"
            className={cn(
              "px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
              tab === tabKey
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
            onClick={() => setTab(tabKey)}
          >
            {t(`tabs.${tabKey}`)} ({tabKey === "core" ? coreSkills.length : customSkills.length})
          </button>
        ))}
      </div>

      <div className="mt-4">
        <MissingDepsPanel missing={allMissing} onInstallItem={installSingleDep} runtimes={tab === "core" ? runtimes : undefined} />
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
          {tab === "custom" && (
            <div className="text-sm text-muted-foreground">
              {t("folder.selectionCount", {
                defaultValue: "{{count}} skills selected",
                count: selectedCustomSkills.length,
              })}
            </div>
          )}
        </div>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Zap}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  {tab === "custom" && (
                    <th className="px-4 py-3 text-left font-medium w-10">
                      <input
                        type="checkbox"
                        checked={allPageSelected}
                        onChange={(e) => toggleSelectPage(e.target.checked)}
                        className="accent-primary"
                        aria-label={t("actions.selectAllPage", { defaultValue: "Select all skills on page" })}
                      />
                    </th>
                  )}
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.description")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.author")}</th>}
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.visibility")}</th>}
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((skill: SkillInfo, index: number) => {
                  const folderKey = getSkillFolderKey(skill, tab);
                  const prevSkill = index > 0 ? pageItems[index - 1] : null;
                  const prevFolderKey = prevSkill ? getSkillFolderKey(prevSkill, tab) : null;
                  const showFolderHeader = tab === "custom" && folderKey !== prevFolderKey;
                  return (
                    <Fragment key={skill.id ?? skill.name}>
                      {showFolderHeader && (
                        <tr className="border-b bg-muted/30">
                          <td colSpan={columnCount} className="px-4 py-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                            {folderKey === "__ungrouped__"
                              ? t("folder.ungrouped", { defaultValue: "Ungrouped" })
                              : folderKey}
                          </td>
                        </tr>
                      )}
                      <SkillTableRow
                        skill={skill}
                        tab={tab}
                        hasTenantScope={hasTenantScope}
                        toggling={toggling}
                        selected={!!skill.id && selectedSkillIds.includes(skill.id)}
                        onSelectChange={toggleSkillSelection}
                        onView={handleViewSkill}
                        onEdit={setEditTarget}
                        onDelete={setDeleteTarget}
                        onToggle={handleToggle}
                        onCycleVisibility={handleCycleVisibility}
                        onSetTenantConfig={handleSetTenantConfig}
                        onDeleteTenantConfig={handleDeleteTenantConfig}
                      />
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
            <Pagination
              page={pagination.page}
              pageSize={pagination.pageSize}
              total={pagination.total}
              totalPages={pagination.totalPages}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        )}
      </div>

      {selectedSkill && (
        <SkillDetailDialog
          skill={selectedSkill}
          onClose={() => setSelectedSkill(null)}
          initialTab={detailInitialTab}
          getSkillVersions={getSkillVersions}
          getSkillFiles={getSkillFiles}
          getSkillFileContent={getSkillFileContent}
          updateSkillFileContent={updateSkillFileContent}
          onSaved={refresh}
        />
      )}

      {editTarget && (
        <SkillEditDialog
          skill={editTarget}
          onClose={() => setEditTarget(null)}
          onSave={async (id, updates) => { await updateSkill(id, updates); setEditTarget(null); }}
        />
      )}

      <Suspense fallback={null}>
        <SkillUploadDialog open={uploadOpen} onOpenChange={setUploadOpen} onUpload={(f) => uploadSkill(f)} />
      </Suspense>

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />

      <Dialog open={folderDialogOpen} onOpenChange={setFolderDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {folderDialogMode === "create"
                ? t("folder.createTitle", { defaultValue: "Create folder and move skills" })
                : t("folder.moveTitle", { defaultValue: "Move selected skills" })}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="bulk-folder-name">
                {t("folder.nameLabel", { defaultValue: "Folder name" })}
              </Label>
              <Input
                id="bulk-folder-name"
                value={folderInput}
                onChange={(e) => setFolderInput(e.target.value)}
                placeholder={t("folder.namePlaceholder", { defaultValue: "brands/pizza-hips or shared/content" })}
                list="skill-folder-options"
              />
              <datalist id="skill-folder-options">
                {existingFolderOptions.map((folder) => (
                  <option key={folder} value={folder} />
                ))}
              </datalist>
              <p className="text-xs text-muted-foreground">
                {t("folder.helper", {
                  defaultValue: "Folders appear when at least one skill is assigned to them.",
                })}
              </p>
            </div>
            <div className="rounded-md border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
              {t("folder.selectedSummary", {
                defaultValue: "{{count}} selected skills will be moved into this folder.",
                count: selectedCustomSkills.length,
              })}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setFolderDialogOpen(false)} disabled={folderSaving}>
              {t("edit.cancel")}
            </Button>
            <Button onClick={handleApplyFolder} disabled={folderSaving || !folderInput.trim() || selectedCustomSkills.length === 0}>
              {folderSaving && <Loader2 className="h-4 w-4 animate-spin" />}
              {folderDialogMode === "create"
                ? t("folder.createConfirm", { defaultValue: "Create and Move" })
                : t("folder.moveConfirm", { defaultValue: "Move Skills" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function getSkillFolderKey(skill: SkillInfo, tab: Tab): string {
  if (tab !== "custom") return "__system__";
  const folder = skill.folder?.trim();
  return folder ? folder : "__ungrouped__";
}
