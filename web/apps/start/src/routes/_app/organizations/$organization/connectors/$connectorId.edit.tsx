import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { ScopedConnectorEdit } from '@/features/connectors/scoped-connector-form';
import {
  prefetchConnector,
  prefetchConnectorAgents,
} from '@/server/prefetch';

const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;

/** Search for the form route: the launching route to return to (sanitized on read). */
function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/connectors/$connectorId/edit',
)({
  validateSearch: validateFormSearch,
  /**
   * SSR-prefetch the single (org-direct) connector record + the agent options in
   * PARALLEL, priming each under the byte-identical `$api` key the client hooks
   * read — so the record is in the server HTML and no XHR fires on load.
   */
  loader: async ({ context, params }) => {
    if (typeof window !== 'undefined') return;
    const [connector, agents] = await Promise.all([
      prefetchConnector({
        data: { orgSlug: params.organization, connectorId: params.connectorId },
      }),
      prefetchConnectorAgents({ data: { orgSlug: params.organization } }),
    ]);

    if (connector) {
      const { queryKey } = $api.queryOptions('get', CONNECTOR_PATH, {
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
  const { organization, connectorId } = Route.useParams();
  const { from } = Route.useSearch();
  return (
    <ScopedConnectorEdit
      orgSlug={organization}
      connectorId={connectorId}
      from={from}
    />
  );
}
