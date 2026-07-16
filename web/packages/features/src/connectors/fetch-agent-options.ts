import { parseResourceName } from '@pivox/client';

import { toAgentOptions } from '@/connectors/agent-options';

import type { ApiClient } from '@pivox/client';
import type { AgentOption } from '@pivox/ui/resource-admin';

const GATEWAYS_PATH =
  '/v1/organizations/{organization}/storageGateways' as const;
const AGENTS_PATH =
  '/v1/organizations/{organization}/storageGateways/{storageGateway}/agents' as const;

/**
 * Fans out gateways → per-gateway agents for an org and flattens them into
 * dropdown options. Agents nest under storage gateways (no flat org agent list),
 * so this is the composite fetch. Router- and react-query-agnostic: it takes an
 * openapi-fetch `ApiClient`, so the browser client and the SSR server client
 * both drive it.
 */
export async function fetchAgentOptions(
  client: ApiClient,
  organization: string,
): Promise<AgentOption[]> {
  const gwResp = await client.GET(GATEWAYS_PATH, {
    params: { path: { organization } },
  });
  const gateways = gwResp.data?.storageGateways ?? [];
  const perGateway = await Promise.all(
    gateways.map(async (gateway) => {
      const storageGateway = gateway.name
        ? (parseResourceName(gateway.name).storageGateways ?? '')
        : '';
      if (!storageGateway) return [];
      const agentsResp = await client.GET(AGENTS_PATH, {
        params: { path: { organization, storageGateway } },
      });
      return agentsResp.data?.agents ?? [];
    }),
  );
  return toAgentOptions(perGateway.flat());
}
