import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { KeyValueEditor } from "@/components/shared/key-value-editor";
import { slugify } from "@/lib/slug";
import type { MCPFormData } from "@/schemas/mcp.schema";

/** Header keys whose values should be masked in the form. */
const SENSITIVE_HEADER_RE = /^(authorization|x-api-key|api-key|bearer|token|secret|password|credential)/i;
export const isSensitiveHeader = (key: string) => SENSITIVE_HEADER_RE.test(key.trim());

const TRANSPORTS = [
  { value: "stdio", label: "stdio" },
  { value: "sse", label: "SSE" },
  { value: "streamable-http", label: "Streamable HTTP" },
] as const;

interface McpConnectionFieldsProps {
  form: UseFormReturn<MCPFormData>;
}

/** Renders transport selector plus stdio command/args or HTTP url/headers fields. */
export function McpConnectionFields({ form }: McpConnectionFieldsProps) {
  const { t } = useTranslation("mcp");
  const { register, watch, setValue, formState: { errors } } = form;
  const transport = watch("transport");
  const name = watch("name");
  const headers = watch("headers") as Record<string, string>;
  const preset = watch("preset");
  const url = watch("url");
  const isStdio = transport === "stdio";

  return (
    <>
      <div className="grid gap-1.5">
        <Label htmlFor="mcp-name">{t("form.name")}</Label>
        <Input
          id="mcp-name"
          value={name}
          onChange={(e) => setValue("name", slugify(e.target.value))}
          placeholder="my-mcp-server"
        />
        {errors.name ? (
          <p className="text-xs text-destructive">{errors.name.message}</p>
        ) : (
          <p className="text-xs text-muted-foreground">{t("form.nameHint")}</p>
        )}
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="mcp-display">{t("form.displayName")}</Label>
        <Input id="mcp-display" placeholder={t("form.displayNamePlaceholder")} {...register("displayName")} />
      </div>

      <div className="grid gap-1.5">
        <Label>{t("form.transport")}</Label>
        <div className="flex gap-2">
          {TRANSPORTS.map((tr) => (
            <Button
              key={tr.value}
              type="button"
              variant={transport === tr.value ? "default" : "outline"}
              size="sm"
              onClick={() => setValue("transport", tr.value)}
            >
              {tr.label}
            </Button>
          ))}
        </div>
      </div>

      <div className="grid gap-1.5">
        <Label>MCP type</Label>
        <div className="flex gap-2">
          <Button
            type="button"
            variant={preset === "generic" ? "default" : "outline"}
            size="sm"
            onClick={() => setValue("preset", "generic")}
          >
            Generic
          </Button>
          <Button
            type="button"
            variant={preset === "facebook" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setValue("preset", "facebook");
              if (transport === "stdio") setValue("transport", "sse");
              if (!url.trim()) setValue("url", "http://facebook-mcp:3100/sse");
            }}
          >
            Facebook
          </Button>
          <Button
            type="button"
            variant={preset === "google_drive" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setValue("preset", "google_drive");
              if (transport === "stdio") setValue("transport", "sse");
              if (!url.trim()) setValue("url", "http://google-drive-mcp:3200/sse");
            }}
          >
            Google Drive
          </Button>
        </div>
      </div>

      {isStdio ? (
        <>
          <div className="grid gap-1.5">
            <Label htmlFor="mcp-cmd">{t("form.command")}</Label>
            <Input id="mcp-cmd" placeholder="npx" className="font-mono" {...register("command")} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="mcp-args">{t("form.args")}</Label>
            <Input id="mcp-args" placeholder={t("form.argsPlaceholder")} className="font-mono" {...register("args")} />
          </div>
        </>
      ) : (
        <>
          <div className="grid gap-1.5">
            <Label htmlFor="mcp-url">{t("form.url")}</Label>
            <Input id="mcp-url" placeholder="http://localhost:3001/sse" className="font-mono" {...register("url")} />
          </div>
          <div className={preset === "facebook" || preset === "google_drive" ? "grid gap-1.5 rounded-md border border-border p-3" : "grid gap-1.5"}>
            <Label>{t("form.headers")}</Label>
            {preset === "facebook" && <p className="text-xs text-muted-foreground">Advanced headers are generated from the Facebook page form.</p>}
            {preset === "google_drive" && <p className="text-xs text-muted-foreground">Advanced headers are generated from the Google Drive form.</p>}
            <KeyValueEditor
              value={headers}
              onChange={(v) => setValue("headers", v)}
              keyPlaceholder={t("form.headerKeyPlaceholder")}
              valuePlaceholder={t("form.headerValuePlaceholder")}
              addLabel={t("form.addHeader")}
              maskValue={isSensitiveHeader}
            />
          </div>
        </>
      )}
    </>
  );
}
