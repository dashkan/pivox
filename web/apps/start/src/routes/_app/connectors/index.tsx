import { organizationId } from '@pivox/client';
import {
  ConnectorsFeature,
  fetchAgentOptions,
} from '@pivox/features/connectors';
import { useAppShellContext } from '@pivox/ui/app-shell';
import {
  AdminNotice,
  connectorSpaceSlug,
  leafId,
} from '@pivox/ui/resource-admin';
import { createFileRoute, useRouterState } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useCallback, useMemo } from 'react';

import type { Connector, ListControlsChange } from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import {
  searchToValue,
  validateConnectorsSearch,
  valueToSearch,
} from '@/lib/connectors-search';
import { prefetchConnectorAgents, prefetchConnectors } from '@/server/prefetch';

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;

// Agent membership changes rarely; keep the SSR-primed options fresh so no
// gateways/agents XHR fires on page load (a stale-time-0 hydration would refetch).
const AGENTS_STALE_TIME = 5 * 60 * 1000;

export const Route = createFileRoute('/_app/connectors/')({
  validateSearch: validateConnectorsSearch,
  // Re-run the loader when the list controls change, so SSR loads (and links)
  // for a filtered/sorted/paged URL are prefetched for that exact query.
  loaderDeps: ({ search }) => search,
  /**
   * SSR-only prefetch. On the server pass, build the same connectors request the
   * client hook will (shared `buildConnectorsListRequest`), fetch it as the
   * user, and prime the QueryClient under the byte-identical react-query key so
   * the rows are in the server-rendered HTML. Also prime the composite
   * agent-options query so no gateways/agents XHR fires on load. Client
   * navigations skip this — the client's own queries fetch, and
   * `keepPreviousData` avoids a flash.
   */
  loader: async ({ context, deps }) => {
    if (typeof window !== 'undefined') return;
    // Both prefetches read the active-org cookie/session independently, so run
    // them in parallel rather than waterfalling the agents fetch on the
    // connectors result.
    const [prefetched, agents] = await Promise.all([
      prefetchConnectors({ data: deps }),
      prefetchConnectorAgents(),
    ]);

    if (prefetched) {
      const connectionPath = prefetched.isSpaceScoped
        ? SPACE_CONNECTORS_PATH
        : CONNECTORS_PATH;
      const pathParams = prefetched.isSpaceScoped
        ? prefetched.pathParams
        : { organization: prefetched.orgSlug };
      const { queryKey } = $api.queryOptions('get', connectionPath, {
        params: {
          path: pathParams,
          query: prefetched.query,
        },
      });
      context.queryClient.setQueryData(queryKey, prefetched.connectors);
    }

    if (agents) {
      context.queryClient.setQueryData(
        connectorAgentsQueryKey(agents.orgSlug),
        agents.options,
      );
    }
  },
  component: ConnectorsPage,
});

function ConnectorsPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const orgSlug = parent ? organizationId(parent) : '';
  // Single composite agent-options query, SSR-primed by the loader under the
  // same key. Replaces the per-mount fan-out that used to live in the feature.
  const agentsQuery = useQuery({
    queryKey: connectorAgentsQueryKey(orgSlug),
    queryFn: () => fetchAgentOptions(apiClient, orgSlug),
    enabled: Boolean(orgSlug),
    staleTime: AGENTS_STALE_TIME,
  });
  const agentOptions = useMemo(
    () => agentsQuery.data ?? [],
    [agentsQuery.data],
  );

  const listState = useMemo(() => searchToValue(search), [search]);
  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      // Discrete changes push a history entry (Back works); debounced search
      // text replaces so keystrokes don't clutter history. All router-driven.
      void navigate({
        search: valueToSearch(next),
        replace: opts.history === 'replace',
      });
    },
    [navigate],
  );

  // Capture THIS list view's exact URL so the form pages can return to it
  // (filters/scope/page/scroll are already encoded here). The `?from=` param is
  // the single source both create-scope and return-target flow from.
  const currentHref = useRouterState({
    select: (s) => s.location.pathname + s.location.searchStr,
  });
  const onCreate = useCallback(() => {
    void navigate({ to: '/connectors/new', search: { from: currentHref } });
  }, [navigate, currentHref]);
  const onEdit = useCallback(
    (connector: Connector) => {
      const space = connectorSpaceSlug(connector.name);
      void navigate({
        to: '/connectors/$connectorId/edit',
        params: { connectorId: leafId(connector.name) },
        search: { from: currentHref, ...(space ? { space } : {}) },
      });
    },
    [navigate, currentHref],
  );

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to manage connectors.</AdminNotice>
      </div>
    );
  }

  return (
    <ConnectorsFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      listState={listState}
      onListStateChange={onListStateChange}
      agentOptions={agentOptions}
      onCreate={onCreate}
      onEdit={onEdit}
    />
  );
}
