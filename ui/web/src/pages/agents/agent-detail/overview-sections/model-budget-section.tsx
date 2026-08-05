import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { DollarSign } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";

// Quick reasoning-effort levels shown inline next to the model picker. Advanced
// options (auto/none/minimal + fallback) still live in the Advanced dialog;
// this control edits the same reasoning_config and stays non-destructive to those.
const BASE_EFFORTS = ["inherit", "off", "low", "medium", "high"];

// Tiers above "high" are appended inline when the selected model's capability
// metadata advertises them (e.g. GPT-5.6: xhigh/max/ultra, Luna without ultra).
const HIGH_TIERS = ["xhigh", "max", "ultra"];

interface ModelBudgetSectionProps {
  provider: string;
  onProviderChange: (v: string) => void;
  model: string;
  onModelChange: (v: string) => void;
  contextWindow: number;
  onContextWindowChange: (v: number) => void;
  maxToolIterations: number;
  onMaxToolIterationsChange: (v: number) => void;
  savedProvider: string;
  savedModel: string;
  budgetDollars: string;
  onBudgetDollarsChange: (v: string) => void;
  onSaveBlockedChange?: (blocked: boolean) => void;
  effort?: string;
  onEffortChange?: (v: string) => void;
}

export function ModelBudgetSection({
  provider, onProviderChange, model, onModelChange,
  contextWindow, onContextWindowChange,
  maxToolIterations, onMaxToolIterationsChange,
  savedProvider, savedModel,
  budgetDollars, onBudgetDollarsChange,
  onSaveBlockedChange,
  effort, onEffortChange,
}: ModelBudgetSectionProps) {
  const { t } = useTranslation("agents");

  const handleSaveBlockedChange = useCallback((blocked: boolean) => {
    onSaveBlockedChange?.(blocked);
  }, [onSaveBlockedChange]);

  // Resolve the selected model's reasoning capability (react-query dedupes the
  // /models fetch with ProviderModelSelect below).
  const { providers } = useProviders();
  const providerId = providers.find((p) => p.name === provider)?.id;
  const { models } = useProviderModels(providerId);
  const capability = models.find(
    (m) => m.id === model || model.endsWith(`/${m.id}`),
  )?.reasoning ?? null;
  const supportedHighTiers = HIGH_TIERS.filter(
    (lvl) => capability?.levels?.includes(lvl),
  );

  // Base + model-supported high tiers; keep any advanced-set effort (e.g.
  // "minimal") selectable so the inline picker never silently drops it.
  const knownOptions = [...BASE_EFFORTS, ...supportedHighTiers];
  const effortOptions = effort && !knownOptions.includes(effort)
    ? [...knownOptions, effort]
    : knownOptions;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.modelBudget")}</h3>

      <ProviderModelSelect
        provider={provider}
        onProviderChange={onProviderChange}
        model={model}
        onModelChange={onModelChange}
        savedProvider={savedProvider}
        savedModel={savedModel}
        onSaveBlockedChange={handleSaveBlockedChange}
        providerTip="LLM provider name. Must match a configured provider."
        modelTip="Model ID to use."
      />

      {onEffortChange ? (
        <div className="space-y-1.5">
          <Label htmlFor="reasoningEffort" className="text-xs">
            {t("configSections.thinking.thinkingLevel")}
          </Label>
          <Select value={effort || "inherit"} onValueChange={onEffortChange}>
            <SelectTrigger id="reasoningEffort" className="w-full text-base sm:w-56 md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {effortOptions.map((lvl) => (
                <SelectItem key={lvl} value={lvl}>
                  {t(`configSections.thinking.${lvl}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="contextWindow" className="text-xs">{t("llmConfig.contextWindow")}</Label>
          <Input
            id="contextWindow"
            type="number"
            value={contextWindow || ""}
            onChange={(e) => onContextWindowChange(Number(e.target.value) || 0)}
            placeholder="200000"
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("llmConfig.contextWindowHint")}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="maxToolIterations" className="text-xs">{t("llmConfig.maxToolIterations")}</Label>
          <Input
            id="maxToolIterations"
            type="number"
            value={maxToolIterations || ""}
            onChange={(e) => onMaxToolIterationsChange(Number(e.target.value) || 0)}
            placeholder="25"
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("llmConfig.maxToolIterationsHint")}</p>
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="budget" className="text-xs">
          <span className="flex items-center gap-1">
            <DollarSign className="h-3 w-3 text-emerald-500" />
            {t("general.budgetLabel")}
          </span>
        </Label>
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">$</span>
          <Input
            id="budget"
            type="number"
            min="0"
            step="0.01"
            placeholder="0.00"
            value={budgetDollars}
            onChange={(e) => onBudgetDollarsChange(e.target.value)}
            className="max-w-[200px] text-base md:text-sm"
          />
        </div>
        <p className="text-xs text-muted-foreground">{t("general.budgetHint")}</p>
      </div>
    </section>
  );
}
