import { parseResourceName } from '@pivox/client';

import type { components } from '@pivox/client/types';
import type { AgentOption } from '@pivox/ui/resource-admin';

type Agent = components['schemas']['v1Agent'];

/**
 * Flatten agents (gathered across an org's storage gateways) into dropdown
 * options: value = the agent resource name, label = the agent's hostname, or
 * its id leaf when the hostname is unreported. Agents without a name are
 * dropped — the name is the selectable identity.
 */
export function toAgentOptions(agents: Agent[]): AgentOption[] {
  const out: AgentOption[] = [];
  for (const agent of agents) {
    if (!agent.name) continue;
    const leaf = parseResourceName(agent.name).agents ?? agent.name;
    out.push({ value: agent.name, label: agent.hostname || leaf });
  }
  return out;
}
