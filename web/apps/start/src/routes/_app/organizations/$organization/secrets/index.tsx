import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { validateSecretsSearch } from '@/lib/secrets-search';
import { ScopedSecretsList } from '@/features/secrets/scoped-secrets-list';
import { prefetchSecrets } from '@/server/prefetch';

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;

export const Route = createFileRoute(
  '/_app/organizations/$organization/secrets/',
)({
  validateSearch: validateSecretsSearch,
  // Re-run the loader when the list controls change, so SSR loads (and links) for
  // a filtered/sorted/paged URL are prefetched for that exact query.
  loaderDeps: ({ search }) => search,
  /**
   * SSR-only prefetch. Build the same request the client hook will (scope pinned
   * from the path — org rollup here), fetch it as the user, and prime the
   * QueryClient under the byte-identical react-query key so the rows are in the
   * server HTML. Org now comes from the PATH param, not a cookie.
   */
  loader: async ({ context, params, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchSecrets({
      data: { orgSlug: params.organization, search: deps },
    });
    if (prefetched) {
      const { queryKey } = $api.queryOptions('get', SECRETS_PATH, {
        params: {
          path: { organization: prefetched.orgSlug },
          query: prefetched.query,
        },
      });
      context.queryClient.setQueryData(queryKey, prefetched.secrets);
    }
  },
  component: SecretsIndexPage,
});

function SecretsIndexPage() {
  const { organization } = Route.useParams();
  const search = Route.useSearch();
  return <ScopedSecretsList orgSlug={organization} search={search} />;
}
