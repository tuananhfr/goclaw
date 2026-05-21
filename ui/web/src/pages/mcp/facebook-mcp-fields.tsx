import { useEffect, useRef, useState } from "react";
import type { UseFormReturn } from "react-hook-form";
import { ImagePlus, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Slider } from "@/components/ui/slider";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useHttp } from "@/hooks/use-ws";
import type { MCPFormData } from "@/schemas/mcp.schema";

type FacebookPageFormValue = MCPFormData["facebookPages"][number];
type WatermarkFormValue = NonNullable<FacebookPageFormValue["watermark"]>;
type CommentScheduleFormValue = NonNullable<FacebookPageFormValue["comment_schedule"]>;

const defaultWatermark: WatermarkFormValue = {
  enabled: true,
  mode: "logo",
  text: "",
  logo_path: "",
  logo_url: "",
  logo_preview_url: "",
  x_pct: 0.5,
  y_pct: 0.12,
  scale_pct: 0.18,
  opacity: 0.45,
};

const defaultCommentSchedule: CommentScheduleFormValue = {
  enabled: false,
  comment_count: 5,
  window_ms: 30 * 60 * 1000,
  min_gap_ms: 60 * 1000,
  random_order: true,
};

interface FacebookMcpFieldsProps {
  form: UseFormReturn<MCPFormData>;
  serverId?: string;
}

export function FacebookMcpFields({ form, serverId }: FacebookMcpFieldsProps) {
  const http = useHttp();
  const { watch, setValue, getValues } = form;
  const signedPreviewPaths = useRef(new Set<string>());
  const pages = watch("facebookPages") ?? [];

  useEffect(() => {
    pages.forEach((page, pageIdx) => {
      getWatermarks(page).forEach((wm, wmIdx) => {
        if (!wm.logo_path || signedPreviewPaths.current.has(wm.logo_path)) return;
        signedPreviewPaths.current.add(wm.logo_path);
        void http.post<{ url: string }>("/v1/files/sign", { path: wm.logo_path })
          .then((res) => {
            const latest = getValues("facebookPages") ?? [];
            const next = latest.map((p, i) => i === pageIdx
              ? { ...p, watermarks: updateWatermarkList(getWatermarks(p), wmIdx, { logo_preview_url: res.url }) }
              : p);
            setValue("facebookPages", next, { shouldDirty: false });
          })
          .catch(() => undefined);
      });
    });
  }, [pages, http, getValues, setValue]);

  const updatePage = (idx: number, patch: Partial<FacebookPageFormValue>) => {
    setValue("facebookPages", pages.map((p, i) => (i === idx ? { ...p, ...patch } : p)), { shouldDirty: true });
  };

  const updateWatermark = (pageIdx: number, wmIdx: number, patch: Partial<WatermarkFormValue>) => {
    const page = pages[pageIdx];
    if (!page) return;
    updatePage(pageIdx, { watermarks: updateWatermarkList(getWatermarks(page), wmIdx, patch), watermark: undefined });
  };

  const updateCommentSchedule = (pageIdx: number, patch: Partial<CommentScheduleFormValue>) => {
    const page = pages[pageIdx];
    if (!page) return;
    updatePage(pageIdx, { comment_schedule: { ...defaultCommentSchedule, ...page.comment_schedule, ...patch } });
  };

  const addWatermark = (pageIdx: number) => {
    const page = pages[pageIdx];
    if (!page) return;
    updatePage(pageIdx, { watermarks: [...getWatermarks(page), { ...defaultWatermark }], watermark: undefined });
  };

  const removeWatermark = (pageIdx: number, wmIdx: number) => {
    const page = pages[pageIdx];
    if (!page) return;
    updatePage(pageIdx, { watermarks: getWatermarks(page).filter((_, i) => i !== wmIdx), watermark: undefined });
  };

  const addPage = () => {
    setValue("facebookPages", [
      ...pages,
      { page_id: "", name: "", access_token: "", watermarks: [{ ...defaultWatermark }], comment_schedule: { ...defaultCommentSchedule } },
    ], { shouldDirty: true });
  };

  const removePage = (idx: number) => {
    setValue("facebookPages", pages.filter((_, i) => i !== idx), { shouldDirty: true });
  };

  const uploadLogo = async (pageIdx: number, wmIdx: number, file: File) => {
    const formData = new FormData();
    formData.set("file", file);
    const path = serverId ? `/v1/mcp/servers/${serverId}/assets/watermark` : "/v1/mcp/assets/watermark";
    const res = await http.upload<{ path: string; url_path?: string; url: string }>(path, formData);
    updateWatermark(pageIdx, wmIdx, {
      logo_path: res.path,
      logo_url: res.url_path ?? res.path,
      logo_preview_url: res.url,
    });
  };

  return (
    <div className="grid gap-3 rounded-md border border-border p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <Label>Facebook pages</Label>
          <p className="text-xs text-muted-foreground">Configure page credentials, scheduled comments, and watermark placement.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={addPage} className="gap-1.5">
          <Plus className="h-3.5 w-3.5" /> Add page
        </Button>
      </div>

      {pages.length === 0 && (
        <Button type="button" variant="secondary" onClick={addPage}>Add first Facebook page</Button>
      )}

      {pages.map((page, pageIdx) => {
        const watermarks = getWatermarks(page);
        return (
          <div key={pageIdx} className="grid gap-3 border-t border-border pt-3">
            <div className="flex items-center justify-between">
              <Label>Page {pageIdx + 1}</Label>
              <Button type="button" variant="ghost" size="icon" onClick={() => removePage(pageIdx)}>
                <Trash2 className="h-4 w-4 text-muted-foreground" />
              </Button>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <Input
                value={page.page_id}
                onChange={(e) => updatePage(pageIdx, { page_id: e.target.value })}
                placeholder="Page ID"
                className="font-mono"
              />
              <Input
                value={page.name ?? ""}
                onChange={(e) => updatePage(pageIdx, { name: e.target.value })}
                placeholder="Page name"
              />
            </div>
            <Input
              type="password"
              value={page.access_token ?? ""}
              onChange={(e) => updatePage(pageIdx, { access_token: e.target.value })}
              placeholder="Page access token"
              className="font-mono"
            />

            <CommentScheduleFields
              value={{ ...defaultCommentSchedule, ...page.comment_schedule }}
              onChange={(patch) => updateCommentSchedule(pageIdx, patch)}
            />

            <div className="flex items-center justify-between gap-3">
              <Label>Watermarks</Label>
              <Button type="button" variant="outline" size="sm" onClick={() => addWatermark(pageIdx)} className="gap-1.5">
                <Plus className="h-3.5 w-3.5" /> Add watermark
              </Button>
            </div>

            {watermarks.map((wm, wmIdx) => (
              <div key={wmIdx} className="grid gap-3 rounded-md border border-border p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <Switch checked={wm.enabled} onCheckedChange={(v) => updateWatermark(pageIdx, wmIdx, { enabled: v })} />
                    <Label>Watermark {wmIdx + 1}</Label>
                  </div>
                  <Button type="button" variant="ghost" size="icon" onClick={() => removeWatermark(pageIdx, wmIdx)}>
                    <Trash2 className="h-4 w-4 text-muted-foreground" />
                  </Button>
                </div>

                {wm.enabled && (
                  <div className="grid gap-3">
                    <Tabs value={wm.mode} onValueChange={(v) => updateWatermark(pageIdx, wmIdx, { mode: v as "logo" | "text" })}>
                      <TabsList>
                        <TabsTrigger value="logo">Logo</TabsTrigger>
                        <TabsTrigger value="text">Text</TabsTrigger>
                      </TabsList>
                    </Tabs>

                    {wm.mode === "logo" ? (
                      <LogoUpload onUpload={(file) => uploadLogo(pageIdx, wmIdx, file)} />
                    ) : (
                      <Input
                        value={wm.text ?? ""}
                        onChange={(e) => updateWatermark(pageIdx, wmIdx, { text: e.target.value })}
                        placeholder="Watermark text"
                      />
                    )}

                    <WatermarkPreview watermark={wm} onChange={(patch) => updateWatermark(pageIdx, wmIdx, patch)} />

                    <div className="grid gap-3 sm:grid-cols-2">
                      <WatermarkPercentControl
                        label="Size"
                        value={wm.scale_pct}
                        min={0.06}
                        max={0.5}
                        step={0.01}
                        onChange={(scale_pct) => updateWatermark(pageIdx, wmIdx, { scale_pct })}
                      />
                      <WatermarkPercentControl
                        label="Opacity"
                        value={wm.opacity}
                        min={0.1}
                        max={1}
                        step={0.05}
                        onChange={(opacity) => updateWatermark(pageIdx, wmIdx, { opacity })}
                      />
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

function CommentScheduleFields({
  value,
  onChange,
}: {
  value: CommentScheduleFormValue;
  onChange: (patch: Partial<CommentScheduleFormValue>) => void;
}) {
  return (
    <div className="grid gap-3 rounded-md border border-border p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <Label>Scheduled comments</Label>
          <p className="text-xs text-muted-foreground">Store the page policy in Facebook MCP; GoClaw schedules final comments after posting.</p>
        </div>
        <Switch checked={value.enabled} onCheckedChange={(enabled) => onChange({ enabled })} />
      </div>

      {value.enabled && (
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <Label>Comment count</Label>
            <Input
              type="number"
              min={1}
              max={50}
              value={value.comment_count}
              onChange={(e) => onChange({ comment_count: Number(e.target.value) })}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>Window minutes</Label>
            <Input
              type="number"
              min={1}
              value={Math.round(value.window_ms / 60000)}
              onChange={(e) => onChange({ window_ms: Number(e.target.value) * 60000 })}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>Min gap seconds</Label>
            <Input
              type="number"
              min={0}
              value={Math.round(value.min_gap_ms / 1000)}
              onChange={(e) => onChange({ min_gap_ms: Number(e.target.value) * 1000 })}
            />
          </div>
          <div className="flex items-center gap-2 pt-6">
            <Switch checked={value.random_order} onCheckedChange={(random_order) => onChange({ random_order })} />
            <Label>Random order</Label>
          </div>
        </div>
      )}
    </div>
  );
}

function getWatermarks(page: FacebookPageFormValue): WatermarkFormValue[] {
  if (page.watermarks?.length) return page.watermarks.map((wm) => ({ ...defaultWatermark, ...wm }));
  if (page.watermark) return [{ ...defaultWatermark, ...page.watermark }];
  return [{ ...defaultWatermark }];
}

function updateWatermarkList(
  list: WatermarkFormValue[],
  idx: number,
  patch: Partial<WatermarkFormValue>,
): WatermarkFormValue[] {
  return list.map((wm, i) => i === idx ? { ...defaultWatermark, ...wm, ...patch } : wm);
}

function LogoUpload({ onUpload }: { onUpload: (file: File) => void }) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  return (
    <div>
      <input
        ref={inputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) onUpload(file);
        }}
      />
      <Button type="button" variant="outline" size="sm" onClick={() => inputRef.current?.click()} className="gap-1.5">
        <ImagePlus className="h-3.5 w-3.5" /> Upload logo
      </Button>
    </div>
  );
}

function WatermarkPercentControl({
  label,
  value,
  min,
  max,
  step,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (value: number) => void;
}) {
  const pct = Math.round(value * 100);

  return (
    <div className="grid gap-1.5">
      <div className="flex items-center justify-between">
        <Label>{label}</Label>
        <span className="text-sm font-medium text-muted-foreground">{pct}%</span>
      </div>
      <Slider value={[value]} min={min} max={max} step={step} onValueChange={([v]) => onChange(v ?? value)} />
    </div>
  );
}

function WatermarkPreview({
  watermark,
  onChange,
}: {
  watermark: WatermarkFormValue;
  onChange: (patch: Partial<WatermarkFormValue>) => void;
}) {
  const [dragging, setDragging] = useState(false);
  const boxRef = useRef<HTMLDivElement | null>(null);
  const left = `${watermark.x_pct * 100}%`;
  const top = `${watermark.y_pct * 100}%`;
  const width = `${watermark.scale_pct * 100}%`;

  const updateFromPointer = (clientX: number, clientY: number) => {
    const rect = boxRef.current?.getBoundingClientRect();
    if (!rect) return;
    const x = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    const y = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height));
    onChange({ x_pct: x, y_pct: y });
  };

  return (
    <div
      ref={boxRef}
      className="relative aspect-square w-full max-w-sm overflow-hidden rounded-md border border-dashed border-border bg-[linear-gradient(45deg,hsl(var(--muted))_25%,transparent_25%),linear-gradient(-45deg,hsl(var(--muted))_25%,transparent_25%),linear-gradient(45deg,transparent_75%,hsl(var(--muted))_75%),linear-gradient(-45deg,transparent_75%,hsl(var(--muted))_75%)] bg-[length:24px_24px] bg-[position:0_0,0_12px,12px_-12px,-12px_0]"
      onPointerMove={(e) => dragging && updateFromPointer(e.clientX, e.clientY)}
      onPointerUp={() => setDragging(false)}
      onPointerLeave={() => setDragging(false)}
    >
      <div
        className="absolute cursor-move select-none touch-none"
        style={{
          left,
          top,
          width,
          transform: "translate(-50%, -50%)",
          opacity: watermark.opacity,
        }}
        onPointerDown={(e) => {
          setDragging(true);
          (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
          updateFromPointer(e.clientX, e.clientY);
        }}
      >
        {watermark.mode === "logo" && watermark.logo_preview_url ? (
          <img src={watermark.logo_preview_url} alt="" className="block w-full" draggable={false} />
        ) : (
          <div className="w-full rounded border bg-background/80 px-2 py-1 text-center text-sm font-semibold">
            {watermark.mode === "text" ? (watermark.text || "Watermark") : "Logo"}
          </div>
        )}
      </div>
    </div>
  );
}
