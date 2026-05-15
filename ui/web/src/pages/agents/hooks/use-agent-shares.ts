import { useCallback, useState } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import type { AgentShareData } from "@/types/agent";

export function useAgentShares(agentId: string | undefined) {
  const http = useHttp();
  const [shares, setShares] = useState<AgentShareData[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const res = await http.get<{ shares: AgentShareData[] }>(`/v1/agents/${agentId}/shares`);
      setShares(res.shares ?? []);
    } catch {
      setShares([]);
    } finally {
      setLoading(false);
    }
  }, [agentId, http]);

  const share = useCallback(
    async (userId: string, role: string) => {
      if (!agentId) return;
      try {
        await http.post(`/v1/agents/${agentId}/shares`, { user_id: userId, role });
        toast.success("Agent shared");
        await load();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to share agent");
      }
    },
    [agentId, http, load],
  );

  const revoke = useCallback(
    async (userId: string) => {
      if (!agentId) return;
      try {
        await http.delete(`/v1/agents/${agentId}/shares/${encodeURIComponent(userId)}`);
        toast.success("Agent share revoked");
        await load();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to revoke share");
      }
    },
    [agentId, http, load],
  );

  return { shares, loading, load, share, revoke };
}
