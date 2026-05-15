import { useEffect, useMemo, useState } from "react";
import { Loader2, Plus, RefreshCw, Share2, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useAgentShares } from "../hooks/use-agent-shares";

interface AgentSharesSectionProps {
  agentId: string;
}

const SHARE_ROLES = ["user", "operator", "admin"] as const;

export function AgentSharesSection({ agentId }: AgentSharesSectionProps) {
  const { t } = useTranslation("agents");
  const { shares, loading, load, share, revoke } = useAgentShares(agentId);
  const [userId, setUserId] = useState("");
  const [role, setRole] = useState("user");
  const [adding, setAdding] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null);
  const [revoking, setRevoking] = useState(false);

  useEffect(() => { load(); }, [load]);

  const userIds = useMemo(() => [...new Set(shares.map((s) => s.user_id))], [shares]);
  const { resolve } = useContactResolver(userIds);

  const handleShare = async () => {
    const trimmed = userId.trim();
    if (!trimmed) return;
    setAdding(true);
    try {
      await share(trimmed, role);
      setUserId("");
      setRole("user");
    } finally {
      setAdding(false);
    }
  };

  const handleRevoke = async () => {
    if (!revokeTarget) return;
    setRevoking(true);
    try {
      await revoke(revokeTarget);
      setRevokeTarget(null);
    } finally {
      setRevoking(false);
    }
  };

  return (
    <section className="space-y-4 rounded-lg border p-3 sm:p-4">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium flex items-center gap-2">
            <Share2 className="h-4 w-4 text-blue-500" />
            {t("shares.grantAccess")}
          </h3>
          <p className="text-xs text-muted-foreground mt-1">{t("shares.noSharesDesc")}</p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 shrink-0 text-muted-foreground"
          onClick={load}
          disabled={loading}
        >
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </div>

      <div className="flex flex-wrap items-end gap-2">
        <UserPickerCombobox
          value={userId}
          onChange={setUserId}
          placeholder={t("shares.userIdPlaceholder")}
          className="flex-1 min-w-[180px]"
          source="tenant_user"
          allowCustom={true}
        />
        <Select value={role} onValueChange={setRole}>
          <SelectTrigger className="w-[130px] text-base md:text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SHARE_ROLES.map((r) => (
              <SelectItem key={r} value={r}>{t(`shares.role.${r}`, { defaultValue: r })}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="icon"
          className="h-9 w-9 shrink-0"
          onClick={handleShare}
          disabled={adding || !userId.trim()}
          title={t("shares.share")}
        >
          {adding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
        </Button>
      </div>

      {loading && shares.length === 0 ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : shares.length === 0 ? (
        <p className="text-xs text-muted-foreground text-center py-6">{t("shares.noShares")}</p>
      ) : (
        <div className="rounded-lg border divide-y">
          {shares.map((shareItem) => (
            <div key={shareItem.id} className="flex items-center justify-between gap-2 px-3 py-2">
              <div className="min-w-0">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-sm font-medium truncate">
                    {formatUserLabel(shareItem.user_id, resolve)}
                  </span>
                  <Badge variant="secondary" className="text-2xs shrink-0">
                    {t(`shares.role.${shareItem.role}`, { defaultValue: shareItem.role })}
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground truncate">{shareItem.user_id}</p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
                onClick={() => setRevokeTarget(shareItem.user_id)}
                title={t("shares.revoke")}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={!!revokeTarget}
        onOpenChange={(open) => { if (!open) setRevokeTarget(null); }}
        title={t("shares.revokeTitle")}
        description={t("shares.revokeDesc", { userId: revokeTarget ?? "" })}
        confirmLabel={t("shares.revoke")}
        variant="destructive"
        onConfirm={handleRevoke}
        loading={revoking}
      />
    </section>
  );
}
