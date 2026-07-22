import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { validateConnectorsSearch } from '@/lib/connectors-search';
import { ScopedConnectorsList } from '@/features/connectors/scoped-connectors-list';
import { prefetchConnectorAgents, prefetchConnectors } from '@/server/prefetch';

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/connectors/',
)({
  validateSearch: validateConnectorsSearch,
  // Re-run the loader when the list controls change, so SSR loads (and links) for
  // a filtered/sorted/paged URL are prefetched for that exact query.
  loaderDeps: ({ search }) => search,
  /**
   * SSR-only prefetch. Build the same request the client hook will (scope pinned
   * from the path — org rollup here), fetch it as the user, and prime the
   * QueryClient under the byte-identical react-query key so the rows are in the
   * server HTML. Also prime the composite agent-options query. Org + space now
   * come from PATH params, not a cookie.
   */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const [prefetched, agents] = await Promise.all([
      prefetchConnectors({ data: { orgSlug: params.organization, search: deps } }),
      prefetchConnectorAgents({ data: { orgSlug: params.organization } }),
    ]);

    if (prefetched) {
      const { queryKey } = $api.queryOptions('get', CONNECTORS_PATH, {
        params: {
          path: { organization: prefetched.orgSlug },
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
  component: ConnectorsIndexPage,
});

function ConnectorsIndexPage() {
  const { organization } = Route.useParams();
  const search = Route.useSearch();
  return <ScopedConnectorsList orgSlug={organization} search={search} />;
}
