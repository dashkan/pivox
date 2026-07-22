import { createFileRoute } from '@tanstack/react-router';

import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { ScopedConnectorNew } from '@/features/connectors/scoped-connector-form';
import { prefetchConnectorAgents } from '@/server/prefetch';

/** Search for the form route: the launching route to return to (sanitized on read). */
function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/connectors/new',
)({
  validateSearch: validateFormSearch,
  /** SSR-prime the composite agent-options query so the "Run on Agent" field renders without an XHR. */
  loader: async ({ context, params }) => {
    if (typeof window !== 'undefined') return;
    const agents = await prefetchConnectorAgents({
      data: { orgSlug: params.organization },
    });
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
  const { organization } = Route.useParams();
  const { from } = Route.useSearch();
  return <ScopedConnectorNew orgSlug={organization} from={from} />;
}
