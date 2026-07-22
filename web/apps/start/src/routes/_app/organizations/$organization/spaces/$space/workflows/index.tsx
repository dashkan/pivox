import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { validateWorkflowsSearch } from '@/lib/workflows-search';
import { ScopedWorkflowsList } from '@/features/workflows/scoped-workflows-list';
import { prefetchWorkflows } from '@/server/prefetch';

const SPACE_WORKFLOWS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/workflows/',
)({
  validateSearch: validateWorkflowsSearch,
  loaderDeps: ({ search }) => search,
  /** SSR-only prefetch, scope pinned to this space from the path param. */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchWorkflows({
      data: { orgSlug: params.organization, space: params.space, search: deps },
    });
    if (prefetched && prefetched.isSpaceScoped) {
      const { queryKey } = $api.queryOptions('get', SPACE_WORKFLOWS_PATH, {
        params: { path: prefetched.pathParams, query: prefetched.query },
      });
      context.queryClient.setQueryData(queryKey, prefetched.workflows);
    }
  },
  component: SpaceWorkflowsIndexPage,
});

function SpaceWorkflowsIndexPage() {
  const { organization, space } = Route.useParams();
  const search = Route.useSearch();
  return (
    <ScopedWorkflowsList orgSlug={organization} spaceSlug={space} search={search} />
  );
}
