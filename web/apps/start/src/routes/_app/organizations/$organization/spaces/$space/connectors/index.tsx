import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { validateConnectorsSearch } from '@/lib/connectors-search';
import { ScopedConnectorsList } from '@/features/connectors/scoped-connectors-list';
import { prefetchConnectorAgents, prefetchConnectors } from '@/server/prefetch';

const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/connectors/',
)({
  validateSearch: validateConnectorsSearch,
  loaderDeps: ({ search }) => search,
  /** SSR-only prefetch, scope pinned to this space from the path param. */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const [prefetched, agents] = await Promise.all([
      prefetchConnectors({
        data: { orgSlug: params.organization, space: params.space, search: deps },
      }),
      prefetchConnectorAgents({ data: { orgSlug: params.organization } }),
    ]);

    if (prefetched && prefetched.isSpaceScoped) {
      const { queryKey } = $api.queryOptions('get', SPACE_CONNECTORS_PATH, {
        params: { path: prefetched.pathParams, query: prefetched.query },
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
  component: SpaceConnectorsIndexPage,
});

function SpaceConnectorsIndexPage() {
  const { organization, space } = Route.useParams();
  const search = Route.useSearch();
  return (
    <ScopedConnectorsList orgSlug={organization} spaceSlug={space} search={search} />
  );
}
