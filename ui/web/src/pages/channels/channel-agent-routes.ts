import type { AgentData } from "@/types/agent";

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function agentRouteRefs(agent: AgentData | undefined, agentId: string): Set<string> {
  const refs = new Set<string>([agentId]);
  if (agent?.agent_key) refs.add(agent.agent_key);
  return refs;
}

export function syncDiscordRoutesForAgentChange(
  channelType: string,
  config: Record<string, unknown>,
  oldAgentId: string,
  newAgentId: string,
  agents: AgentData[],
): Record<string, unknown> {
  if (channelType !== "discord" || oldAgentId === newAgentId) return config;
  const routes = config.channel_agent_routes;
  if (!isRecord(routes)) return config;

  const oldRefs = agentRouteRefs(agents.find((a) => a.id === oldAgentId), oldAgentId);
  let changed = false;
  const nextRoutes: Record<string, unknown> = {};

  for (const [channelId, agentRef] of Object.entries(routes)) {
    if (typeof agentRef === "string" && oldRefs.has(agentRef.trim())) {
      nextRoutes[channelId] = newAgentId;
      changed = true;
    } else {
      nextRoutes[channelId] = agentRef;
    }
  }

  return changed ? { ...config, channel_agent_routes: nextRoutes } : config;
}
