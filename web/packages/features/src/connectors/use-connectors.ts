'use client';

import { useListControls } from '@pivox/ui/resource-admin';
import { useCallback, useMemo, useState } from 'react';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  AgentOption,
  Connector,
  ConnectorsAdminContextValue,
  ListControlsChange,
  ListControlsValue,
  RemoveState,
} from '@pivox/ui/resource-admin';

import { buildConnectorsListRequest } from '@/connectors/build-connectors-request';
import { deleteConnector } from '@/connectors/save-connector';
import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import { useSpaces } from '@/spaces/use-spaces';
import { resourcePathParams } from '@/workflows/resource-paths';

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a
 * new query key (filter / scope / page change) loads — no empty flash, no
 * `isLoading` flip that would unmount the filter controls. Equivalent to
 * react-query's `keepPreviousData`, inlined to avoid a direct dep on it.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

const initialRemove: RemoveState<Connector> = {
  target: null,
  error: null,
  pending: false,
};

/**
 * Drives the Connectors admin LIST surface. Reads via the injected `$api`
 * (openapi-react-query) and writes via the injected `apiClient` (openapi-fetch)
 * — the app-shell / create-org precedent. `parent` is the org resource name
 * (`organizations/{slug}`); its path params flow to both the list and item
 * routes.
 *
 * Create/edit are now ROUTED pages, not a dialog: `onCreate` / `onEdit`
 * (route-injected navigation, setting `?from=<origin>`) become the list's
 * `openCreate` / `openEdit`. The quick row-delete confirm stays here; the edit
 * page has its own `FormPage.Delete`.
 */
export function useConnectors(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** All assignable agents (route-owned, SSR-prefetched); passed to the form pages. */
  agentOptions: AgentOption[];
  /** Navigate to the routed create page (route-owned; sets `?from=`). */
  onCreate: () => void;
  /** Navigate to the routed edit page for a connector (route-owned; sets `?from=`). */
  onEdit: (connector: Connector) => void;
}): ConnectorsAdminContextValue {
  const {
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    agentOptions,
    onCreate,
    onEdit,
  } = input;
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
  const { query } = buildConnectorsListRequest(path.organization, listState);
  const scope = listState.scope;

  // Scope switches the list parent path. Both queries are declared so the hook
  // count stays stable; only the one matching the scope is enabled.
  const orgListQuery = $api.useQuery(
    'get',
    CONNECTORS_PATH,
    { params: { path, query } },
    { enabled: scope === '', placeholderData: keepPrevious },
  );
  const spaceListQuery = $api.useQuery(
    'get',
    SPACE_CONNECTORS_PATH,
    { params: { path: { organization: path.organization, space: scope }, query } },
    { enabled: scope !== '', placeholderData: keepPrevious },
  );
  const listQuery = scope === '' ? orgListQuery : spaceListQuery;

  const connectors = useMemo<Connector[]>(
    () => listQuery.data?.connectors ?? [],
    [listQuery.data],
  );

  // Distinct non-empty agents present in the base scope (server-computed, NOT
  // narrowed by the filter). Sources the agent FILTER facet.
  const agentsInUse = useMemo<string[]>(
    () => listQuery.data?.agentsInUse ?? [],
    [listQuery.data],
  );

  const [remove, setRemove] = useState<RemoveState<Connector>>(initialRemove);

  const { refetch } = listQuery;

  const openRemove = useCallback((connector: Connector) => {
    setRemove({ ...initialRemove, target: connector });
  }, []);

  const closeRemove = useCallback(() => {
    setRemove((r) => ({ ...r, target: null }));
  }, []);

  const confirmRemove = useCallback(() => {
    const target = remove.target;
    if (!target?.name) return;
    setRemove((r) => ({ ...r, pending: true, error: null }));
    void (async () => {
      const resp = await deleteConnector({ apiClient, connector: target });
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
      connectors,
      isLoading: listQuery.isLoading,
      loadError: listQuery.error
        ? describeRpcError(listQuery.error, "Couldn't load connectors.")
        : null,
      agentOptions,
      agentsInUse,
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
