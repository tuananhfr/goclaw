import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { ChannelFields } from "../channel-fields";
import { configSchema } from "../channel-schemas";
import type { ChannelInstanceData } from "@/types/channel";
import type { AgentData } from "@/types/agent";
import { flattenConfig, unflattenConfig } from "@/lib/config-flatten";

interface ChannelAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instance: ChannelInstanceData;
  agents?: AgentData[];
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const ESSENTIAL_CONFIG_KEYS = new Set(["dm_policy", "group_policy", "require_mention", "mention_mode"]);

const NETWORK_KEYS = new Set(["api_server", "proxy", "domain", "connection_mode", "webhook_port", "webhook_path", "webhook_url"]);
const LIMITS_KEYS = new Set(["history_limit", "media_max_mb", "text_chunk_limit", "comment_reply_options.max_thread_depth"]);
const STREAMING_KEYS = new Set(["dm_stream", "group_stream", "draft_transport", "reasoning_stream", "native_stream", "debounce_delay", "thread_ttl"]);
const BEHAVIOR_KEYS = new Set([
  "reaction_level",
  "link_preview",
  "block_reply",
  "render_mode",
  "topic_session_mode",
  "features.comment_reply",
  "features.messenger_auto_reply",
  "features.first_inbox",
  "comment_reply_options.include_post_context",
  "messenger_options.session_timeout",
  "post_context_cache_ttl",
  "first_inbox_message",
]);
const ACCESS_KEYS = new Set(["allow_from", "group_allow_from", "allowed_channels", "channel_agent_routes"]);

function getAdvancedFields(channelType: string) {
  const allFields = configSchema[channelType] ?? [];
  const advanced = allFields.filter((f) => !ESSENTIAL_CONFIG_KEYS.has(f.key));
  return {
    network: advanced.filter((f) => NETWORK_KEYS.has(f.key)),
    limits: advanced.filter((f) => LIMITS_KEYS.has(f.key)),
    streaming: advanced.filter((f) => STREAMING_KEYS.has(f.key)),
    behavior: advanced.filter((f) => BEHAVIOR_KEYS.has(f.key)),
    access: advanced.filter((f) => ACCESS_KEYS.has(f.key)),
  };
}

function flattenFields(groups: ReturnType<typeof getAdvancedFields>) {
  return Object.values(groups).flat();
}

function isEmptyAdvancedValue(value: unknown) {
  if (value === undefined || value === "" || value === null) return true;
  if (Array.isArray(value)) return value.length === 0;
  if (typeof value === "object") return Object.keys(value as Record<string, unknown>).length === 0;
  return false;
}

function deriveInitialValues(instance: ChannelInstanceData): Record<string, unknown> {
  const config = flattenConfig((instance.config ?? {}) as Record<string, unknown>);
  // Only keep advanced keys (exclude essential + groups)
  return Object.fromEntries(
    Object.entries(config).filter(([k]) => !ESSENTIAL_CONFIG_KEYS.has(k) && k !== "groups"),
  );
}

export function ChannelAdvancedDialog({
  open,
  onOpenChange,
  instance,
  agents = [],
  onUpdate,
}: ChannelAdvancedDialogProps) {
  const { t } = useTranslation("channels");
  const groups = getAdvancedFields(instance.channel_type);

  const [values, setValues] = useState<Record<string, unknown>>(() => deriveInitialValues(instance));
  const [saving, setSaving] = useState(false);

  // Re-sync local state when dialog opens
  useEffect(() => {
    if (!open) return;
    setValues(deriveInitialValues(instance));
     
  }, [open, instance]);

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const existingConfig = flattenConfig((instance.config ?? {}) as Record<string, unknown>);
      const advancedKeys = new Set(flattenFields(groups).map((f) => f.key));
      const cleanAdvanced = Object.fromEntries(
        Object.entries(values).filter(([, v]) => !isEmptyAdvancedValue(v)),
      );
      const baseConfig = Object.fromEntries(
        Object.entries(existingConfig).filter(([key]) => !advancedKeys.has(key)),
      );
      // Preserve essential keys and groups from existing, then replace the whole advanced slice.
      // This lets users clear a tags field instead of keeping stale values from the old config.
      const merged = { ...baseConfig, ...cleanAdvanced };
      await onUpdate({ config: unflattenConfig(merged) });
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  const hasAnyGroup = Object.values(groups).some((g) => g.length > 0);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advancedTitle")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {!hasAnyGroup && (
            <p className="text-sm text-muted-foreground">{t("detail.config.noSchema")}</p>
          )}

          {groups.network.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.network")}
                description={t("detail.networkDesc")}
              />
              <ChannelFields
                fields={groups.network}
                values={values}
                onChange={handleChange}
                idPrefix="adv-net"
                contextValues={values}
                agents={agents}
              />
            </>
          )}

          {groups.limits.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.limits")}
                description={t("detail.limitsDesc")}
              />
              <ChannelFields
                fields={groups.limits}
                values={values}
                onChange={handleChange}
                idPrefix="adv-lim"
                agents={agents}
              />
            </>
          )}

          {groups.streaming.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.streaming")}
                description={t("detail.streamingDesc")}
              />
              <ChannelFields
                fields={groups.streaming}
                values={values}
                onChange={handleChange}
                idPrefix="adv-str"
                agents={agents}
              />
            </>
          )}

          {groups.behavior.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.behavior")}
                description={t("detail.behaviorDesc")}
              />
              <ChannelFields
                fields={groups.behavior}
                values={values}
                onChange={handleChange}
                idPrefix="adv-beh"
                agents={agents}
              />
            </>
          )}

          {groups.access.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.accessControl")}
                description={t("detail.accessControlDesc")}
              />
              <ChannelFields
                fields={groups.access}
                values={values}
                onChange={handleChange}
                idPrefix="adv-acc"
                agents={agents}
              />
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? t("form.saving") : t("detail.config.saveConfig")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
