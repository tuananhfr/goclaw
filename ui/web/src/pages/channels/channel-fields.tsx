import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { MultiUserPicker } from "@/components/shared/multi-user-picker";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToolNameSelect } from "@/components/shared/tool-name-select";
import { SkillNameSelect } from "@/components/shared/skill-name-select";
import type { FieldDef } from "./channel-schemas";
import type { AgentData } from "@/types/agent";

const INHERIT = "__inherit__";

interface ChannelFieldsProps {
  fields: FieldDef[];
  values: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
  idPrefix: string;
  isEdit?: boolean; // for credentials: show "leave blank to keep" hint
  /** Extra values for showWhen checks (e.g. config values visible to credential fields) */
  contextValues?: Record<string, unknown>;
  agents?: AgentData[];
}

export function ChannelFields({ fields, values, onChange, idPrefix, isEdit, contextValues, agents = [] }: ChannelFieldsProps) {
  const allValues = contextValues ? { ...contextValues, ...values } : values;
  return (
    <div className="grid gap-3">
      {fields.map((field) => {
        // Conditional visibility: skip field if showWhen condition is not met
        if (field.showWhen) {
          const depValue = allValues[field.showWhen.key] ?? fields.find((f) => f.key === field.showWhen!.key)?.defaultValue;
          const depStr = depValue !== undefined && depValue !== null ? String(depValue) : "";
          const match = Array.isArray(field.showWhen.value)
            ? field.showWhen.value.includes(depStr)
            : depStr === field.showWhen.value;
          if (!match) return null;
        }
        // Check disabledWhen condition
        let disabled = false;
        let disabledHint: string | undefined;
        if (field.disabledWhen) {
          const depValue = allValues[field.disabledWhen.key] ?? fields.find((f) => f.key === field.disabledWhen!.key)?.defaultValue;
          if (String(depValue) === field.disabledWhen.value) {
            disabled = true;
            disabledHint = field.disabledWhen.hint;
          }
        }
        return (
          <FieldRenderer
            key={field.key}
            field={field}
            value={values[field.key]}
            onChange={(v) => onChange(field.key, v)}
            id={`${idPrefix}-${field.key}`}
            isEdit={isEdit}
            disabled={disabled}
            disabledHint={disabledHint}
            agents={agents}
          />
        );
      })}
    </div>
  );
}

function FieldRenderer({
  field,
  value,
  onChange,
  id,
  isEdit,
  disabled,
  disabledHint,
  agents,
}: {
  field: FieldDef;
  value: unknown;
  onChange: (v: unknown) => void;
  id: string;
  isEdit?: boolean;
  disabled?: boolean;
  disabledHint?: string;
  agents?: AgentData[];
}) {
  const { t } = useTranslation("channels");
  // i18n: try "fieldConfig.<key>.label" / "fieldConfig.<key>.help", fall back to hardcoded schema string
  const label = t(`fieldConfig.${field.key}.label`, { defaultValue: field.label });
  const help = field.help ? t(`fieldConfig.${field.key}.help`, { defaultValue: field.help }) : "";
  const resolvedHint = disabledHint ? t(disabledHint, { defaultValue: disabledHint }) : undefined;
  const labelSuffix = field.required && !isEdit ? " *" : "";
  const editHint = isEdit && field.type === "password" ? ` ${t("form.credentialsHint")}` : "";

  switch (field.type) {
    case "text":
    case "password":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>
            {label}{labelSuffix}{editHint}
          </Label>
          <Input
            id={id}
            type={field.type}
            value={(value as string) ?? ""}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "number":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}{labelSuffix}</Label>
          <Input
            id={id}
            type="number"
            value={value !== undefined && value !== null ? String(value) : ""}
            onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
            placeholder={field.defaultValue !== undefined ? String(field.defaultValue) : undefined}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "boolean": {
      const boolHint = resolvedHint || help;
      return (
        <div className={`grid gap-1${disabled ? " opacity-50" : ""}`}>
          <div className="flex items-center gap-2">
            <Switch
              id={id}
              checked={(value as boolean) ?? (field.defaultValue as boolean) ?? false}
              onCheckedChange={(v) => onChange(v)}
              disabled={disabled}
            />
            <Label htmlFor={id}>{label}</Label>
          </div>
          {boolHint && <p className="text-xs text-muted-foreground ml-9">{boolHint}</p>}
        </div>
      );
    }

    case "select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}{labelSuffix}</Label>
          <Select
            value={(value as string) ?? (field.defaultValue as string) ?? ""}
            onValueChange={(v) => onChange(v)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {field.options?.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t(`fieldOptions.${field.key}.${opt.value}`, { defaultValue: opt.label })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tristate": {
      // Tri-state: undefined = inherit, value = override.
      // With options: select with Inherit + custom options (string value).
      // Without options: select with Inherit/Yes/No (boolean value).
      const inheritLabel = t("groupOverrides.fields.inherit", { defaultValue: "Inherit" });

      if (field.options) {
        // String tri-state (e.g. group_policy)
        const allOptions = [{ value: INHERIT, label: inheritLabel }, ...field.options];
        const selectValue = (value as string) || INHERIT;
        return (
          <div className="grid gap-1.5">
            <Label>{label}</Label>
            <Select
              value={selectValue}
              onValueChange={(v) => onChange(v === INHERIT ? undefined : v)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allOptions.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.value === INHERIT ? inheritLabel : t(`fieldOptions.${field.key}.${opt.value}`, { defaultValue: opt.label })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {help && <p className="text-xs text-muted-foreground">{help}</p>}
          </div>
        );
      }

      // Boolean tri-state (e.g. require_mention, enabled)
      const yesLabel = t("groupOverrides.fields.yes", { defaultValue: "Yes" });
      const noLabel = t("groupOverrides.fields.no", { defaultValue: "No" });
      const triOptions = [
        { value: INHERIT, label: inheritLabel },
        { value: "true", label: yesLabel },
        { value: "false", label: noLabel },
      ];
      const boolToStr = (v: unknown): string => {
        if (v === undefined || v === null) return INHERIT;
        return v ? "true" : "false";
      };
      const strToBool = (v: string): boolean | undefined => {
        if (v === INHERIT) return undefined;
        return v === "true";
      };

      return (
        <div className={`grid gap-1.5${disabled ? " opacity-50" : ""}`}>
          <Label>{label}</Label>
          <Select value={boolToStr(value)} onValueChange={(v) => onChange(strToBool(v))} disabled={disabled}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {triOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {resolvedHint && <p className="text-xs text-muted-foreground">{resolvedHint}</p>}
          {!resolvedHint && help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );
    }

    case "textarea":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}</Label>
          <Textarea
            id={id}
            value={(value as string) ?? ""}
            onChange={(e) => onChange(e.target.value || undefined)}
            placeholder={field.placeholder}
            rows={3}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tool-select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}</Label>
          <ToolNameSelect
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "skill-select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}</Label>
          <SkillNameSelect
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tags":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}</Label>
          <MultiUserPicker
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder ?? t("groupOverrides.fields.allowedUsersPlaceholder")}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "agent-route-map":
      return (
        <AgentRouteMapField
          id={id}
          label={label}
          help={help}
          value={value}
          agents={agents ?? []}
          onChange={onChange}
        />
      );

    default:
      return null;
  }
}

type RouteRow = {
  id: string;
  channelId: string;
  agentId: string;
};

function AgentRouteMapField({
  id,
  label,
  help,
  value,
  agents,
  onChange,
}: {
  id: string;
  label: string;
  help?: string;
  value: unknown;
  agents: AgentData[];
  onChange: (v: unknown) => void;
}) {
  const { t } = useTranslation("channels");
  const [rows, setRows] = useState<RouteRow[]>(() => routeMapToRows(value));

  useEffect(() => {
    setRows(routeMapToRows(value));
  }, [value]);

  const duplicateChannelIds = useMemo(() => {
    const seen = new Set<string>();
    const dupes = new Set<string>();
    for (const row of rows) {
      const channelId = row.channelId.trim();
      if (!channelId) continue;
      if (seen.has(channelId)) {
        dupes.add(channelId);
      }
      seen.add(channelId);
    }
    return dupes;
  }, [rows]);

  const commitRows = (nextRows: RouteRow[], options: { forceSync?: boolean } = {}) => {
    setRows(nextRows);
    const nextMap: Record<string, string> = {};
    const seen = new Set<string>();
    let canSync = true;
    for (const row of nextRows) {
      const channelId = row.channelId.trim();
      const agentId = row.agentId.trim();
      if (!channelId && !agentId) {
        canSync = false;
        continue;
      }
      if (!channelId || !agentId || seen.has(channelId)) {
        canSync = false;
        continue;
      }
      seen.add(channelId);
      nextMap[channelId] = agentId;
    }
    if (options.forceSync || canSync) {
      onChange(Object.keys(nextMap).length > 0 ? nextMap : undefined);
    }
  };

  const addRoute = () => {
    setRows([...rows, { id: createRouteRowId(), channelId: "", agentId: "" }]);
  };

  const updateRoute = (rowId: string, patch: Partial<RouteRow>) => {
    commitRows(rows.map((row) => (row.id === rowId ? { ...row, ...patch } : row)));
  };

  const removeRoute = (rowId: string) => {
    commitRows(rows.filter((row) => row.id !== rowId), { forceSync: true });
  };

  return (
    <div className="grid gap-2">
      <Label htmlFor={`${id}-route-0`}>{label}</Label>
      <div className="grid gap-2 rounded-md border p-3">
        {rows.length === 0 && (
          <p className="text-sm text-muted-foreground">
            {t("fieldConfig.channel_agent_routes.empty", { defaultValue: "No routes configured." })}
          </p>
        )}
        {rows.map((row, index) => {
          const channelId = row.channelId.trim();
          const duplicate = channelId !== "" && duplicateChannelIds.has(channelId);
          return (
            <div key={row.id} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
              <div className="grid gap-1">
                <Input
                  id={`${id}-route-${index}`}
                  value={row.channelId}
                  onChange={(e) => updateRoute(row.id, { channelId: e.target.value })}
                  placeholder={t("fieldConfig.channel_agent_routes.channelPlaceholder", { defaultValue: "Discord channel ID" })}
                  className="text-base md:text-sm"
                />
                {duplicate && (
                  <p className="text-xs text-destructive">
                    {t("fieldConfig.channel_agent_routes.duplicate", { defaultValue: "This channel ID is duplicated." })}
                  </p>
                )}
              </div>
              <Select value={row.agentId || "__select_agent__"} onValueChange={(agentId) => updateRoute(row.id, { agentId })}>
                <SelectTrigger className="text-base md:text-sm">
                  <SelectValue placeholder={t("fieldConfig.channel_agent_routes.agentPlaceholder", { defaultValue: "Select agent" })} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__select_agent__" disabled>
                    {t("fieldConfig.channel_agent_routes.agentPlaceholder", { defaultValue: "Select agent" })}
                  </SelectItem>
                  {agents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.display_name || agent.agent_key}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => removeRoute(row.id)}
                aria-label={t("fieldConfig.channel_agent_routes.remove", { defaultValue: "Remove route" })}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          );
        })}
        <Button type="button" variant="outline" size="sm" onClick={addRoute} className="w-fit">
          <Plus className="h-4 w-4" />
          {t("fieldConfig.channel_agent_routes.add", { defaultValue: "Add route" })}
        </Button>
      </div>
      {help && <p className="text-xs text-muted-foreground">{help}</p>}
    </div>
  );
}

function routeMapToRows(value: unknown): RouteRow[] {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return [];
  }
  return Object.entries(value as Record<string, unknown>).map(([channelId, agentId], index) => ({
    id: `${channelId}-${index}`,
    channelId,
    agentId: typeof agentId === "string" ? agentId : "",
  }));
}

function createRouteRowId() {
  return `route-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}
