import { organizationId } from '@pivox/client';
import {
  ConnectorEditFeature,
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
import {
  prefetchConnector,
  prefetchConnectorAgents,
} from '@/server/prefetch';

const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

const AGENTS_STALE_TIME = 5 * 60 * 1000;

/** Search: the launching route (`from`) + the connector's space slug (org-direct = absent). */
interface ConnectorEditSearch {
  from?: string;
  space?: string;
}

function validateEditSearch(
  search: Record<string, unknown>,
): ConnectorEditSearch {
  const out: ConnectorEditSearch = {};
  if (typeof search.from === 'string' && search.from) out.from = search.from;
  if (typeof search.space === 'string' && search.space) out.space = search.space;
  return out;
}

export const Route = createFileRoute('/_app/connectors/$connectorId/edit')({
  validateSearch: validateEditSearch,
  // Re-run the loader when the target space changes (org-direct vs space-scoped).
  loaderDeps: ({ search }) => ({ space: search.space }),
  /**
   * SSR-prefetch the single connector record + the agent options, in PARALLEL
   * (`3.7 parallel-fetch`), and prime each under the byte-identical `$api` key
   * the client hooks read — so the record is in the server HTML and no XHR fires
   * on load. Client navigations skip this; `useConnectorForm`'s `keepPreviousData`
   * avoids a flash.
   */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const [connector, agents] = await Promise.all([
      prefetchConnector({
        data: { connectorId: params.connectorId, space: deps.space },
      }),
      prefetchConnectorAgents(),
    ]);

    if (connector) {
      const { queryKey } = connector.space
        ? $api.queryOptions('get', SPACE_CONNECTOR_PATH, {
            params: {
              path: {
                organization: connector.orgSlug,
                space: connector.space,
                connector: connector.connectorId,
              },
            },
          })
        : $api.queryOptions('get', CONNECTOR_PATH, {
            params: {
              path: {
                organization: connector.orgSlug,
                connector: connector.connectorId,
              },
            },
          });
      context.queryClient.setQueryData(queryKey, connector.connector);
    }

    if (agents) {
      context.queryClient.setQueryData(
        connectorAgentsQueryKey(agents.orgSlug),
        agents.options,
      );
    }
  },
  component: ConnectorEditPage,
});

function ConnectorEditPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const { connectorId } = Route.useParams();
  const search = Route.useSearch();

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
        <AdminNotice>Select an organization to edit this connector.</AdminNotice>
      </div>
    );
  }

  return (
    <ConnectorEditFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      connectorId={connectorId}
      space={search.space}
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
