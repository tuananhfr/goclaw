import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { FileContentPanel } from "@/components/shared/file-viewers";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import type { SkillInfo, SkillFile, SkillVersions } from "@/types/skill";
import { buildTree } from "./skill-file-helpers";
import { FileBrowser } from "./skill-file-browser";

interface SkillDetailDialogProps {
  skill: SkillInfo & { content: string };
  onClose: () => void;
  initialTab?: "content" | "files";
  getSkillVersions: (id: string) => Promise<SkillVersions>;
  getSkillFiles: (id: string, version?: number) => Promise<SkillFile[]>;
  getSkillFileContent: (id: string, path: string, version?: number) => Promise<{ content: string; path: string; size: number }>;
  updateSkillFileContent: (id: string, path: string, content: string) => Promise<{ version?: number }>;
  onSaved?: () => void;
}

export function SkillDetailDialog({
  skill,
  onClose,
  initialTab = "content",
  getSkillVersions,
  getSkillFiles,
  getSkillFileContent,
  updateSkillFileContent,
  onSaved,
}: SkillDetailDialogProps) {
  const { t } = useTranslation("skills");
  const hasFiles = !!skill.id;

  // Version state
  const [versions, setVersions] = useState<SkillVersions | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(null);

  // File tree state
  const [files, setFiles] = useState<SkillFile[]>([]);
  const [filesLoading, setFilesLoading] = useState(false);
  const [activePath, setActivePath] = useState<string | null>(null);

  // File content state
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [editedContent, setEditedContent] = useState("");
  const [saving, setSaving] = useState(false);
  const autoOpenedSkillMD = useRef(false);

  const tree = useMemo(() => buildTree(files), [files]);

  const loadVersions = useCallback(async () => {
    if (!skill.id || versions) return;
    const v = await getSkillVersions(skill.id);
    setVersions(v);
    setSelectedVersion(v.current);
  }, [skill.id, versions, getSkillVersions]);

  const loadFiles = useCallback(async (version?: number) => {
    if (!skill.id) return;
    setFilesLoading(true);
    try {
      const f = await getSkillFiles(skill.id, version);
      setFiles(f);
      setActivePath(null);
      setFileContent(null);
    } finally {
      setFilesLoading(false);
    }
  }, [skill.id, getSkillFiles]);

  const loadFileContent = useCallback(async (path: string) => {
    if (!skill.id) return;
    setActivePath(path);
    setContentLoading(true);
    try {
      const c = await getSkillFileContent(skill.id, path, selectedVersion ?? undefined);
      setFileContent(c);
      setEditedContent(c.content);
    } finally {
      setContentLoading(false);
    }
  }, [skill.id, selectedVersion, getSkillFileContent]);

  const isCurrentVersion = selectedVersion == null || selectedVersion === (versions?.current ?? skill.version);
  const isActiveSkill = (skill.status ?? "active") === "active";
  const canEditSkillMD = !!skill.id
    && !skill.is_system
    && isActiveSkill
    && isCurrentVersion
    && activePath === "SKILL.md"
    && !!fileContent;
  const hasChanges = fileContent ? editedContent !== fileContent.content : false;

  const handleSaveSkillMD = async () => {
    if (!skill.id || !fileContent || !canEditSkillMD) return;
    setSaving(true);
    try {
      const res = await updateSkillFileContent(skill.id, "SKILL.md", editedContent);
      onSaved?.();
      const refreshed = await getSkillVersions(skill.id);
      setVersions(refreshed);
      setSelectedVersion(res.version ?? refreshed.current);
      setFileContent(null);
      setActivePath(null);
    } finally {
      setSaving(false);
    }
  };

  useEffect(() => {
    if (selectedVersion != null) {
      loadFiles(selectedVersion);
    }
  }, [selectedVersion, loadFiles]);

  useEffect(() => {
    autoOpenedSkillMD.current = false;
  }, [skill.id, selectedVersion]);

  useEffect(() => {
    if (initialTab !== "files" || autoOpenedSkillMD.current || activePath || contentLoading) return;
    if (!files.some((f) => f.path === "SKILL.md")) return;
    autoOpenedSkillMD.current = true;
    loadFileContent("SKILL.md");
  }, [initialTab, files, activePath, contentLoading, loadFileContent]);

  useEffect(() => {
    if (initialTab !== "files" || !hasFiles) return;
    loadVersions();
    if (files.length === 0 && !filesLoading) {
      loadFiles(selectedVersion ?? undefined);
    }
  }, [initialTab, hasFiles, loadVersions, files.length, filesLoading, loadFiles, selectedVersion]);

  const handleTabChange = (tab: string) => {
    if (tab === "files" && hasFiles) {
      loadVersions();
      if (files.length === 0 && !filesLoading) {
        loadFiles(selectedVersion ?? undefined);
      }
    }
  };

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] md:min-h-[60vh] overflow-hidden flex flex-col sm:max-w-2xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl 2xl:max-w-7xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 flex-wrap">
            {skill.name}
            <Badge variant="outline">{skill.source || "file"}</Badge>
            {skill.visibility && (
              <Badge variant="secondary">{skill.visibility}</Badge>
            )}
            {skill.version ? (
              <span className="text-xs font-normal text-muted-foreground">v{skill.version}</span>
            ) : null}
          </DialogTitle>
          {skill.description && (
            <p className="text-sm text-muted-foreground">{skill.description}</p>
          )}
          {skill.tags && skill.tags.length > 0 && (
            <div className="flex flex-wrap gap-1 pt-1">
              {skill.tags.map((tag) => (
                <Badge key={tag} variant="outline" className="text-xs">{tag}</Badge>
              ))}
            </div>
          )}
        </DialogHeader>

        <Tabs defaultValue={initialTab} className="flex-1 overflow-hidden flex flex-col" onValueChange={handleTabChange}>
          <TabsList>
            <TabsTrigger value="content">{t("detail.content")}</TabsTrigger>
            {hasFiles && <TabsTrigger value="files">{t("detail.files")}</TabsTrigger>}
          </TabsList>

          <TabsContent value="content" className="flex-1 overflow-y-auto mt-2 -mx-4 px-4 sm:-mx-6 sm:px-6">
            {skill.content ? (
              <div className="overflow-hidden rounded-md border bg-muted/30 p-4">
                <MarkdownRenderer content={skill.content} />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{t("detail.noContent")}</p>
            )}
          </TabsContent>

          {hasFiles && (
            <TabsContent value="files" className="flex-1 overflow-hidden flex flex-col mt-2 gap-2">
              {versions && versions.versions.length > 1 && (
                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">{t("detail.version")}</span>
                  <Select
                    value={String(selectedVersion ?? versions.current)}
                    onValueChange={(v) => setSelectedVersion(Number(v))}
                  >
                    <SelectTrigger className="w-40 h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {versions.versions.map((v) => (
                        <SelectItem key={v} value={String(v)}>
                          v{v}{v === versions.current ? ` ${t("detail.current")}` : ""}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              <FileBrowser
                tree={tree}
                filesLoading={filesLoading}
                activePath={activePath}
                onSelect={loadFileContent}
                contentLoading={contentLoading}
                fileContent={fileContent}
                renderContent={({ fileContent, contentLoading }) => {
                  if (!canEditSkillMD || !fileContent) {
                    return <FileContentPanel fileContent={fileContent} contentLoading={contentLoading} />;
                  }
                  return (
                    <div className="flex h-full min-h-[360px] flex-col gap-2">
                      <Textarea
                        value={editedContent}
                        onChange={(e) => setEditedContent(e.target.value)}
                        className="min-h-[320px] flex-1 resize-none font-mono text-xs leading-relaxed"
                        spellCheck={false}
                      />
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-xs text-muted-foreground">
                          Saving creates a new skill version.
                        </span>
                        <Button size="sm" onClick={handleSaveSkillMD} disabled={saving || !hasChanges || !editedContent.trim()}>
                          {saving ? "Saving..." : "Save new version"}
                        </Button>
                      </div>
                    </div>
                  );
                }}
              />
            </TabsContent>
          )}
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
