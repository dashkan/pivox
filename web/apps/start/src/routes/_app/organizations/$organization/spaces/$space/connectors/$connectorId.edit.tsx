import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { ScopedConnectorEdit } from '@/features/connectors/scoped-connector-form';
import {
  prefetchConnector,
  prefetchConnectorAgents,
} from '@/server/prefetch';

const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/connectors/$connectorId/edit',
)({
  validateSearch: validateFormSearch,
  /** SSR-prefetch the space-scoped connector record + agent options in parallel. */
  loader: async ({ context, params }) => {
    if (typeof window !== 'undefined') return;
    const [connector, agents] = await Promise.all([
      prefetchConnector({
        data: {
          orgSlug: params.organization,
          space: params.space,
          connectorId: params.connectorId,
        },
      }),
      prefetchConnectorAgents({ data: { orgSlug: params.organization } }),
    ]);

    if (connector && connector.space) {
      const { queryKey } = $api.queryOptions('get', SPACE_CONNECTOR_PATH, {
        params: {
          path: {
            organization: connector.orgSlug,
            space: connector.space,
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
  component: SpaceConnectorEditPage,
});

function SpaceConnectorEditPage() {
  const { organization, space, connectorId } = Route.useParams();
  const { from } = Route.useSearch();
  return (
    <ScopedConnectorEdit
      orgSlug={organization}
      spaceSlug={space}
      connectorId={connectorId}
      from={from}
    />
  );
}
