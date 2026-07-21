'use client';

import { useListControls } from '@pivox/ui/resource-admin';
import { useCallback, useMemo, useState } from 'react';

import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import { resourcePathParams } from '@/workflows/resource-paths';

import type { ListDescriptor } from './list-descriptor';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  RemoveState,
  ResourceListContextValue,
} from '@pivox/ui/resource-admin';

/**
 * Generic, descriptor-driven LIST hook — the router/react-query-agnostic engine
 * behind every admin list. It generalizes the hand-written `useConnectors`:
 * drives the shared list-controls, runs the descriptor's scoped list query,
 * extracts rows / next-cursor / view extras, owns the quick row-delete confirm,
 * and delegates create/edit navigation to the injected callbacks. Reads via the
 * injected `$api`, writes via the injected `apiClient` — no import of either, so
 * `apps/electron` shares it unchanged.
 *
 * The return value IS the DI interface the `@pivox/ui` `ResourceList` consumes;
 * a thin feature wrapper just renders `<ResourceList>` with it.
 */
export function useResourceList<Row, Extras, Injected, Resp>(
  descriptor: ListDescriptor<Row, Extras, Injected, Resp>,
  input: {
    $api: ReactQueryApi;
    apiClient: ApiClient;
    /** Org resource name (`organizations/{slug}`). */
    parent: string;
    /** URL-owned list-controls state. */
    state: ListControlsValue;
    onStateChange: ListControlsChange;
    /** Route-owned extras (SSR-prefetched agents/spaces) merged into the view extras. */
    injected: Injected;
    /** Navigate to the routed create page (route-owned; sets `?from=`). */
    onCreate: () => void;
    /** Navigate to the routed edit page for a row (route-owned; sets `?from=`). */
    onEdit: (row: Row) => void;
  },
): ResourceListContextValue<Row, Extras> {
  const {
    $api,
    apiClient,
    parent,
    state,
    onStateChange,
    injected,
    onCreate,
    onEdit,
  } = input;

  const organization = useMemo(
    () => resourcePathParams(parent).organization ?? '',
    [parent],
  );
  const controls = useListControls({ value: state, onChange: onStateChange });

  const listQuery = descriptor.useList({ $api, organization, state });
  const { data, refetch } = listQuery;

  const rows = useMemo(() => descriptor.rowsOf(data), [descriptor, data]);
  const extras = useMemo(
    () => descriptor.extrasOf(data, injected),
    [descriptor, data, injected],
  );

  const [remove, setRemove] = useState<RemoveState<Row>>({
    target: null,
    error: null,
    pending: false,
  });

  const openRemove = useCallback((row: Row) => {
    setRemove({ target: row, error: null, pending: false });
  }, []);

  const closeRemove = useCallback(() => {
    setRemove((r) => ({ ...r, target: null }));
  }, []);

  const confirmRemove = useCallback(() => {
    const target = remove.target;
    if (!target || !descriptor.rowId(target)) return;
    setRemove((r) => ({ ...r, pending: true, error: null }));
    void (async () => {
      const resp = await descriptor.remove(apiClient, target);
      if (resp.error) {
        setRemove((r) => ({
          ...r,
          pending: false,
          error: mapDeleteError(resp.error),
        }));
        return;
      }
      setRemove({ target: null, error: null, pending: false });
      await refetch();
    })();
  }, [apiClient, remove.target, descriptor, refetch]);

  const nextPageToken = descriptor.nextPageTokenOf(data);
  const { nextPage: pushPage, prevPage } = controls;
  const nextPage = useCallback(() => {
    if (nextPageToken) pushPage(nextPageToken);
  }, [pushPage, nextPageToken]);

  return {
    state: {
      rows,
      isLoading: listQuery.isLoading,
      loadError: listQuery.error
        ? describeRpcError(listQuery.error, descriptor.loadErrorFallback)
        : null,
      remove,
      extras,
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
