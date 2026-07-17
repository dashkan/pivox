import { organizationId } from '@pivox/client';
import {
  ConnectorCreateFeature,
  fetchAgentOptions,
} from '@pivox/features/connectors';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { AdminNotice } from '@pivox/ui/resource-admin';
import { useQuery } from '@tanstack/react-query';
import { createFileRoute } from '@tanstack/react-router';
import { useMemo } from 'react';

import { $api, apiClient } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { useConnectorFormNav } from '@/lib/use-connector-form-nav';
import { prefetchConnectorAgents } from '@/server/prefetch';

// Agent membership changes rarely; keep the SSR-primed options fresh so no
// gateways/agents XHR fires on page load.
const AGENTS_STALE_TIME = 5 * 60 * 1000;

/** Search for the form routes: the launching route to return to (sanitized on read). */
interface ConnectorFormSearch {
  from?: string;
}

function validateFormSearch(
  search: Record<string, unknown>,
): ConnectorFormSearch {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute('/_app/connectors/new')({
  validateSearch: validateFormSearch,
  /**
   * SSR-prime the composite agent-options query (parallel to the list route) so
   * the create form's "Run on Agent" field renders without an XHR on load.
   */
  loader: async ({ context }) => {
    if (typeof window !== 'undefined') return;
    const agents = await prefetchConnectorAgents();
    if (agents) {
      context.queryClient.setQueryData(
        connectorAgentsQueryKey(agents.orgSlug),
        agents.options,
      );
    }
  },
  component: ConnectorNewPage,
});

function ConnectorNewPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const search = Route.useSearch();

  // The route (not FormPage) owns the return target + the soft-navigation dirty
  // guard. FormPage stays router-free — we inject navigate + onDirtyChange.
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } =
    useConnectorFormNav(search.from);

  const orgSlug = parent ? organizationId(parent) : '';
  const agentsQuery = useQuery({
    queryKey: connectorAgentsQueryKey(orgSlug),
    queryFn: () => fetchAgentOptions(apiClient, orgSlug),
    enabled: Boolean(orgSlug),
    staleTime: AGENTS_STALE_TIME,
  });
  const agentOptions = useMemo(() => agentsQuery.data ?? [], [agentsQuery.data]);

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to create a connector.</AdminNotice>
      </div>
    );
  }

  return (
    <ConnectorCreateFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      agentOptions={agentOptions}
      back={
        <a
          href={returnTo}
          className="hover:underline"
          onClick={(e) => {
            e.preventDefault();
            goBack();
          }}
        >
          ← Connectors
        </a>
      }
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}
