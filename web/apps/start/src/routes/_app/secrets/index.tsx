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
  loaderDeps: ({ search }) => search,
  // SSR-only: prefetch this exact query + prime the client's react-query key so
  // rows are in the server HTML. Client navs skip it.
  loader: async ({ context, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchSecrets({ data: deps });
    if (prefetched) {
      const connectionPath = prefetched.isSpaceScoped
        ? SPACE_SECRETS_PATH
        : SECRETS_PATH;
      const { queryKey } = $api.queryOptions('get', connectionPath, {
        params: { path: prefetched.pathParams, query: prefetched.query },
      });
      context.queryClient.setQueryData(queryKey, prefetched.secrets);
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

  // This view's exact URL (filters/scope/page encoded) → the form pages' `?from=` return target.
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
