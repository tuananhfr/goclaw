import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";

/** Codex fast tier is only advertised by the gpt-5.4 / 5.5 / 5.6 families.
 * Mirrors codexFastTierSupported in internal/providers/codex_build.go. */
export function fastTierSupported(model: string): boolean {
  const bare = model.includes("/") ? model.slice(model.lastIndexOf("/") + 1) : model;
  return ["gpt-5.4", "gpt-5.5", "gpt-5.6"].some((p) => bare.startsWith(p));
}

interface FastModeSectionProps {
  enabled: boolean;
  onChange: (v: boolean) => void;
  model: string;
}

/** Toggle for the Codex "fast" service tier (ChatGPT OAuth providers only —
 * the caller gates rendering on provider_type). */
export function FastModeSection({ enabled, onChange, model }: FastModeSectionProps) {
  const { t } = useTranslation("agents");
  const s = "fastMode";
  const supported = fastTierSupported(model);
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
      </div>
      <div className="rounded-lg border p-3 space-y-2 sm:p-4">
        <div className="flex items-center gap-2">
          <Switch checked={enabled} onCheckedChange={onChange} disabled={!supported} />
          <span className="text-sm">{t(`${s}.enable`)}</span>
        </div>
        {!supported ? (
          <p className="text-xs text-muted-foreground">{t(`${s}.unsupportedModel`)}</p>
        ) : null}
      </div>
    </section>
  );
}
