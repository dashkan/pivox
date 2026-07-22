import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { validateWorkflowsSearch } from '@/lib/workflows-search';
import { ScopedWorkflowsList } from '@/features/workflows/scoped-workflows-list';
import { prefetchWorkflows } from '@/server/prefetch';

const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/workflows/',
)({
  validateSearch: validateWorkflowsSearch,
  loaderDeps: ({ search }) => search,
  /**
   * SSR-only prefetch. Build the same request the client hook will (org-direct —
   * workflows have no space variant), fetch it as the user, and prime the
   * QueryClient under the byte-identical react-query key. Org now comes from the
   * PATH param, not a cookie.
   */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchWorkflows({
      data: { orgSlug: params.organization, search: deps },
    });
    if (prefetched) {
      const { queryKey } = $api.queryOptions('get', WORKFLOWS_PATH, {
        params: {
          path: { organization: prefetched.orgSlug },
          query: prefetched.query,
        },
      });
      context.queryClient.setQueryData(queryKey, prefetched.workflows);
    }
  },
  component: WorkflowsIndexPage,
});

function WorkflowsIndexPage() {
  const { organization } = Route.useParams();
  const search = Route.useSearch();
  return <ScopedWorkflowsList orgSlug={organization} search={search} />;
}
