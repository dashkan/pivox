import { SecretsFeature } from '@pivox/features/secrets';
import { useAppShellContext } from '@pivox/ui/app-shell';
import {
  AdminNotice,
  secretLeafId,
  secretSpaceSlug,
} from '@pivox/ui/resource-admin';
import { createFileRoute, useRouterState } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type { ListControlsChange, Secret } from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import {
  searchToValue,
  validateSecretsSearch,
  valueToSearch,
} from '@/lib/secrets-search';
import { prefetchSecrets } from '@/server/prefetch';

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;

export const Route = createFileRoute('/_app/secrets/')({
  validateSearch: validateSecretsSearch,
  // Re-run the loader when the list controls change, so SSR loads (and links)
  // for a filtered/sorted/paged URL are prefetched for that exact query.
  loaderDeps: ({ search }) => search,
  /**
   * SSR-only prefetch. On the server pass, build the same secrets request the
   * client hook will (shared `buildSecretsListRequest`), fetch it as the user,
   * and prime the QueryClient under the byte-identical react-query key so the
   * rows are in the server-rendered HTML. Client navigations skip this.
   */
  loader: async ({ context, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchSecrets({ data: deps });
    if (prefetched) {
      // `isSpaceScoped` discriminates the union, narrowing `pathParams`.
      if (prefetched.isSpaceScoped) {
        const { queryKey } = $api.queryOptions('get', SPACE_SECRETS_PATH, {
          params: { path: prefetched.pathParams, query: prefetched.query },
        });
        context.queryClient.setQueryData(queryKey, prefetched.secrets);
      } else {
        const { queryKey } = $api.queryOptions('get', SECRETS_PATH, {
          params: {
            path: { organization: prefetched.orgSlug },
            query: prefetched.query,
          },
        });
        context.queryClient.setQueryData(queryKey, prefetched.secrets);
      }
    }
  },
  component: SecretsPage,
});

function SecretsPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const listState = useMemo(() => searchToValue(search), [search]);
  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      void navigate({
        search: valueToSearch(next),
        replace: opts.history === 'replace',
      });
    },
    [navigate],
  );

  // Capture THIS list view's exact URL so the form pages can return to it
  // (filters/scope/page are already encoded here). The `?from=` param is the
  // single source both create-scope and return-target flow from.
  const currentHref = useRouterState({
    select: (s) => s.location.pathname + s.location.searchStr,
  });
  const onCreate = useCallback(() => {
    void navigate({ to: '/secrets/new', search: { from: currentHref } });
  }, [navigate, currentHref]);
  const onEdit = useCallback(
    (secret: Secret) => {
      const space = secretSpaceSlug(secret.name);
      void navigate({
        to: '/secrets/$secretId/edit',
        params: { secretId: secretLeafId(secret.name) },
        search: { from: currentHref, ...(space ? { space } : {}) },
      });
    },
    [navigate, currentHref],
  );

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to manage secrets.</AdminNotice>
      </div>
    );
  }

  return (
    <SecretsFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      listState={listState}
      onListStateChange={onListStateChange}
      onCreate={onCreate}
      onEdit={onEdit}
    />
  );
}
