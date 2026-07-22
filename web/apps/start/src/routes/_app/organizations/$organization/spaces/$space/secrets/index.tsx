import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { validateSecretsSearch } from '@/lib/secrets-search';
import { ScopedSecretsList } from '@/features/secrets/scoped-secrets-list';
import { prefetchSecrets } from '@/server/prefetch';

const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/secrets/',
)({
  validateSearch: validateSecretsSearch,
  loaderDeps: ({ search }) => search,
  /** SSR-only prefetch, scope pinned to this space from the path param. */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchSecrets({
      data: { orgSlug: params.organization, space: params.space, search: deps },
    });
    if (prefetched && prefetched.isSpaceScoped) {
      const { queryKey } = $api.queryOptions('get', SPACE_SECRETS_PATH, {
        params: { path: prefetched.pathParams, query: prefetched.query },
      });
      context.queryClient.setQueryData(queryKey, prefetched.secrets);
    }
  },
  component: SpaceSecretsIndexPage,
});

function SpaceSecretsIndexPage() {
  const { organization, space } = Route.useParams();
  const search = Route.useSearch();
  return (
    <ScopedSecretsList orgSlug={organization} spaceSlug={space} search={search} />
  );
}
