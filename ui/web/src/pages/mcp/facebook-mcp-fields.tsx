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

type WatermarkFormValue = NonNullable<MCPFormData["facebookPages"][number]["watermark"]>;

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

interface FacebookMcpFieldsProps {
  form: UseFormReturn<MCPFormData>;
  serverId?: string;
}

export function FacebookMcpFields({ form, serverId }: FacebookMcpFieldsProps) {
  const http = useHttp();
  const { watch, setValue, getValues } = form;
  const pages = watch("facebookPages") ?? [];

  useEffect(() => {
    pages.forEach((page, idx) => {
      const wm = page.watermark;
      if (!wm?.logo_path || wm.logo_preview_url) return;
      void http.post<{ url: string }>("/v1/files/sign", { path: wm.logo_path })
        .then((res) => {
          const latest = getValues("facebookPages") ?? [];
          const next = latest.map((p, i) => i === idx
            ? { ...p, watermark: { ...defaultWatermark, ...(p.watermark ?? {}), logo_preview_url: res.url } }
            : p);
          setValue("facebookPages", next, { shouldDirty: false });
        })
        .catch(() => undefined);
    });
  }, [pages, http, getValues, setValue]);

  const updatePage = (idx: number, patch: Partial<(typeof pages)[number]>) => {
    setValue("facebookPages", pages.map((p, i) => (i === idx ? { ...p, ...patch } : p)), { shouldDirty: true });
  };

  const updateWatermark = (idx: number, patch: Partial<WatermarkFormValue>) => {
    const page = pages[idx];
    if (!page) return;
    updatePage(idx, { watermark: { ...defaultWatermark, ...(page.watermark ?? {}), ...patch } });
  };

  const addPage = () => {
    setValue("facebookPages", [
      ...pages,
      { page_id: "", name: "", access_token: "", watermark: { ...defaultWatermark } },
    ], { shouldDirty: true });
  };

  const removePage = (idx: number) => {
    setValue("facebookPages", pages.filter((_, i) => i !== idx), { shouldDirty: true });
  };

  const uploadLogo = async (idx: number, file: File) => {
    const formData = new FormData();
    formData.set("file", file);
    const path = serverId ? `/v1/mcp/servers/${serverId}/assets/watermark` : "/v1/mcp/assets/watermark";
    const res = await http.upload<{ path: string; url_path?: string; url: string }>(path, formData);
    updateWatermark(idx, { logo_path: res.path, logo_url: res.url_path ?? res.path, logo_preview_url: res.url });
  };

  return (
    <div className="grid gap-3 rounded-md border border-border p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <Label>Facebook pages</Label>
          <p className="text-xs text-muted-foreground">Configure page credentials and watermark placement.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={addPage} className="gap-1.5">
          <Plus className="h-3.5 w-3.5" /> Add page
        </Button>
      </div>

      {pages.length === 0 && (
        <Button type="button" variant="secondary" onClick={addPage}>Add first Facebook page</Button>
      )}

      {pages.map((page, idx) => {
        const wm = { ...defaultWatermark, ...(page.watermark ?? {}) };
        return (
          <div key={idx} className="grid gap-3 border-t border-border pt-3">
            <div className="flex items-center justify-between">
              <Label>Page {idx + 1}</Label>
              <Button type="button" variant="ghost" size="icon" onClick={() => removePage(idx)}>
                <Trash2 className="h-4 w-4 text-muted-foreground" />
              </Button>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <Input
                value={page.page_id}
                onChange={(e) => updatePage(idx, { page_id: e.target.value })}
                placeholder="Page ID"
                className="font-mono"
              />
              <Input
                value={page.name ?? ""}
                onChange={(e) => updatePage(idx, { name: e.target.value })}
                placeholder="Page name"
              />
            </div>
            <Input
              type="password"
              value={page.access_token ?? ""}
              onChange={(e) => updatePage(idx, { access_token: e.target.value })}
              placeholder="Page access token"
              className="font-mono"
            />

            <div className="flex items-center gap-2">
              <Switch checked={wm.enabled} onCheckedChange={(v) => updateWatermark(idx, { enabled: v })} />
              <Label>Watermark</Label>
            </div>

            {wm.enabled && (
              <div className="grid gap-3">
                <Tabs value={wm.mode} onValueChange={(v) => updateWatermark(idx, { mode: v as "logo" | "text" })}>
                  <TabsList>
                    <TabsTrigger value="logo">Logo</TabsTrigger>
                    <TabsTrigger value="text">Text</TabsTrigger>
                  </TabsList>
                </Tabs>

                {wm.mode === "logo" ? (
                  <LogoUpload onUpload={(file) => uploadLogo(idx, file)} />
                ) : (
                  <Input
                    value={wm.text ?? ""}
                    onChange={(e) => updateWatermark(idx, { text: e.target.value })}
                    placeholder="Watermark text"
                  />
                )}

                <WatermarkPreview
                  watermark={wm}
                  onChange={(patch) => updateWatermark(idx, patch)}
                />

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="grid gap-1.5">
                    <Label>Size</Label>
                    <Slider value={[wm.scale_pct]} min={0.06} max={0.5} step={0.01} onValueChange={([v]) => updateWatermark(idx, { scale_pct: v ?? wm.scale_pct })} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Opacity</Label>
                    <Slider value={[wm.opacity]} min={0.1} max={1} step={0.05} onValueChange={([v]) => updateWatermark(idx, { opacity: v ?? wm.opacity })} />
                  </div>
                </div>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
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

function WatermarkPreview({
  watermark,
  onChange,
}: {
  watermark: WatermarkFormValue;
  onChange: (patch: Partial<WatermarkFormValue>) => void;
}) {
  const [dragging, setDragging] = useState(false);
  const boxRef = useRef<HTMLDivElement | null>(null);
  const sizePct = watermark.scale_pct;
  const left = `${watermark.x_pct * 100}%`;
  const top = `${watermark.y_pct * 100}%`;
  const width = `${sizePct * 100}%`;

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
