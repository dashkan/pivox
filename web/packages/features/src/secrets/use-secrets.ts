'use client';

import { useListControls } from '@pivox/ui/resource-admin';
import { useCallback, useMemo, useState } from 'react';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  RemoveState,
  Secret,
  SecretsAdminContextValue,
} from '@pivox/ui/resource-admin';

import { buildSecretsListRequest } from '@/secrets/build-secrets-request';
import { deleteSecret } from '@/secrets/save-secret';
import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import { useSpaces } from '@/spaces/use-spaces';
import { resourcePathParams } from '@/workflows/resource-paths';

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a
 * new query key (filter / scope / page change) loads — no empty flash. Inlined
 * to avoid a direct dep on react-query's `keepPreviousData`.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

const initialRemove: RemoveState<Secret> = {
  target: null,
  error: null,
  pending: false,
};

/**
 * Drives the Secrets admin LIST surface — the secret twin of `useConnectors`.
 * Reads via the injected `$api` and writes via `apiClient`. `parent` is the org
 * resource name; its path params flow to both the list and item routes.
 *
 * Create/edit are ROUTED pages: `onCreate` / `onEdit` (route-injected navigation
 * setting `?from=<origin>`) become the list's `openCreate` / `openEdit`. The
 * quick row-delete confirm stays here; the edit page has its own `FormPage.Delete`.
 */
export function useSecrets(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** Navigate to the routed create page (route-owned; sets `?from=`). */
  onCreate: () => void;
  /** Navigate to the routed edit page for a secret (route-owned; sets `?from=`). */
  onEdit: (secret: Secret) => void;
}): SecretsAdminContextValue {
  const { $api, apiClient, parent, listState, onListStateChange, onCreate, onEdit } =
    input;
  // `parent` is an org resource name; the tested helper yields `{ organization }`.
  const path = useMemo(
    () => ({ organization: resourcePathParams(parent).organization ?? '' }),
    [parent],
  );

  const controls = useListControls({
    value: listState,
    onChange: onListStateChange,
  });
  const { spaces } = useSpaces({ $api, parent });

  // The shared builder produces the exact query the SSR loader keys on.
  const { query } = buildSecretsListRequest(path.organization, listState);
  const scope = listState.scope;

  // Scope switches the list parent path. Both queries are declared so the hook
  // count stays stable; only the one matching the scope is enabled.
  const orgListQuery = $api.useQuery(
    'get',
    SECRETS_PATH,
    { params: { path, query } },
    { enabled: scope === '', placeholderData: keepPrevious },
  );
  const spaceListQuery = $api.useQuery(
    'get',
    SPACE_SECRETS_PATH,
    { params: { path: { organization: path.organization, space: scope }, query } },
    { enabled: scope !== '', placeholderData: keepPrevious },
  );
  const listQuery = scope === '' ? orgListQuery : spaceListQuery;

  const secrets = useMemo<Secret[]>(
    () => listQuery.data?.secrets ?? [],
    [listQuery.data],
  );

  const [remove, setRemove] = useState<RemoveState<Secret>>(initialRemove);

  const { refetch } = listQuery;

  const openRemove = useCallback((secret: Secret) => {
    setRemove({ ...initialRemove, target: secret });
  }, []);

  const closeRemove = useCallback(() => {
    setRemove((r) => ({ ...r, target: null }));
  }, []);

  const confirmRemove = useCallback(() => {
    const target = remove.target;
    if (!target?.name) return;
    setRemove((r) => ({ ...r, pending: true, error: null }));
    void (async () => {
      const resp = await deleteSecret({ apiClient, secret: target });
      if (resp.error) {
        setRemove((r) => ({
          ...r,
          pending: false,
          error: mapDeleteError(resp.error),
        }));
        return;
      }
      setRemove(initialRemove);
      await refetch();
    })();
  }, [apiClient, remove.target, refetch]);

  const nextPageToken = listQuery.data?.nextPageToken;
  const { nextPage: pushPage, prevPage } = controls;
  const nextPage = useCallback(() => {
    if (nextPageToken) pushPage(nextPageToken);
  }, [pushPage, nextPageToken]);

  return {
    state: {
      secrets,
      isLoading: listQuery.isLoading,
      loadError: listQuery.error
        ? describeRpcError(listQuery.error, "Couldn't load secrets.")
        : null,
      spaceOptions: spaces,
      remove,
      filters: controls.filters,
      sort: controls.sort,
      pageSize: controls.pageSize,
      scope: controls.scope,
      pagination: {
        hasPrevPage: controls.hasPrevPage,
        hasNextPage: Boolean(nextPageToken),
      },
    },
    actions: {
      openCreate: onCreate,
      openEdit: onEdit,
      openRemove,
      closeRemove,
      confirmRemove,
      setFilter: controls.setFilter,
      clearFilters: controls.clearFilters,
      toggleSort: controls.toggleSort,
      setPageSize: controls.setPageSize,
      setScope: controls.setScope,
      nextPage,
      prevPage,
    },
  };
}
