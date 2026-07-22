import { createFileRoute } from '@tanstack/react-router';

import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { ScopedConnectorNew } from '@/features/connectors/scoped-connector-form';
import { prefetchConnectorAgents } from '@/server/prefetch';

function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/connectors/new',
)({
  validateSearch: validateFormSearch,
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
  component: SpaceConnectorNewPage,
});

function SpaceConnectorNewPage() {
  const { organization, space } = Route.useParams();
  const { from } = Route.useSearch();
  return <ScopedConnectorNew orgSlug={organization} spaceSlug={space} from={from} />;
}
