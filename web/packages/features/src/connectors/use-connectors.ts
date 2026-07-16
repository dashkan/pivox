'use client';

import { useListControls } from '@pivox/ui/resource-admin';
import { useCallback, useMemo, useState } from 'react';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  ConnectorsAdminContextValue,
  DialogMode,
  DialogState,
  ListControlsChange,
  ListControlsValue,
  RemoveState,
} from '@pivox/ui/resource-admin';

import { buildConnectorBody } from '@/connectors/build-connector-body';
import { buildConnectorsListRequest } from '@/connectors/build-connectors-request';
import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import { useSpaces } from '@/spaces/use-spaces';
import { resourcePathParams } from '@/workflows/resource-paths';

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a
 * new query key (filter / scope / page change) loads — no empty flash, no
 * `isLoading` flip that would unmount the filter controls. Equivalent to
 * react-query's `keepPreviousData`, inlined to avoid a direct dep on it.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

const initialDialog: DialogState<Connector> = {
  open: false,
  mode: 'create',
  editing: null,
  error: null,
  pending: false,
};

const initialRemove: RemoveState<Connector> = {
  target: null,
  error: null,
  pending: false,
};

/**
 * Item-route params for a connector name. `space` is present when the name is
 * space-scoped (`organizations/*​/spaces/*​/connectors/*`), selecting the
 * space-scoped item path; absent selects the org-direct item path.
 */
function connectorItemParams(name: string): {
  organization: string;
  space?: string;
  connector: string;
} {
  const p = resourcePathParams(name);
  return {
    organization: p.organization ?? '',
    space: p.space,
    connector: p.connector ?? '',
  };
}

/**
 * Creates or updates a connector on the path its scope dictates. Create targets
 * the org or the selected space (`values.scope`); update derives the scope from
 * the connector's name (edit can't move scope).
 */
function saveConnector(input: {
  apiClient: ApiClient;
  mode: DialogMode;
  editing: Connector | null;
  organization: string;
  values: ConnectorFormValues;
  body: ReturnType<typeof buildConnectorBody>;
}) {
  const { apiClient, mode, editing, organization, values, body } = input;

  if (mode === 'create') {
    const query = { connectorId: values.connectorId };
    return values.scope
      ? apiClient.POST(SPACE_CONNECTORS_PATH, {
          params: { path: { organization, space: values.scope }, query },
          body,
        })
      : apiClient.POST(CONNECTORS_PATH, {
          params: { path: { organization }, query },
          body,
        });
  }

  const item = connectorItemParams(editing?.name ?? '');
  return item.space
    ? apiClient.PATCH(SPACE_CONNECTOR_PATH, {
        params: {
          path: {
            organization: item.organization,
            space: item.space,
            connector: item.connector,
          },
        },
        body,
      })
    : apiClient.PATCH(CONNECTOR_PATH, {
        params: {
          path: { organization: item.organization, connector: item.connector },
        },
        body,
      });
}

/**
 * Drives the Connectors admin surface. Reads via the injected `$api`
 * (openapi-react-query) and writes via the injected `apiClient` (openapi-fetch)
 * — the app-shell / create-org precedent. `parent` is the org resource name
 * (`organizations/{slug}`); its path params flow to both the list and item
 * routes.
 */
export function useConnectors(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** All assignable agents (route-owned, SSR-prefetched); the create form uses these. */
  agentOptions: AgentOption[];
}): ConnectorsAdminContextValue {
  const { $api, apiClient, parent, listState, onListStateChange, agentOptions } =
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

  const [dialog, setDialog] = useState<DialogState<Connector>>(initialDialog);
  const [remove, setRemove] = useState<RemoveState<Connector>>(initialRemove);

  const { refetch } = listQuery;

  const openCreate = useCallback(() => {
    setDialog({ ...initialDialog, open: true, mode: 'create' });
  }, []);

  const openEdit = useCallback((connector: Connector) => {
    setDialog({ ...initialDialog, open: true, mode: 'edit', editing: connector });
  }, []);

  const closeDialog = useCallback(() => {
    setDialog((d) => ({ ...d, open: false }));
  }, []);

  const submit = useCallback(
    (values: ConnectorFormValues) => {
      setDialog((d) => ({ ...d, pending: true, error: null }));
      void (async () => {
        const body = buildConnectorBody(values);
        const resp = await saveConnector({
          apiClient,
          mode: dialog.mode,
          editing: dialog.editing,
          organization: path.organization,
          values,
          body,
        });
        if (resp.error) {
          setDialog((d) => ({
            ...d,
            pending: false,
            error: describeRpcError(resp.error, "Couldn't save the connector."),
          }));
          return;
        }
        setDialog((d) => ({ ...d, open: false, pending: false }));
        await refetch();
      })();
    },
    [apiClient, dialog.mode, dialog.editing, path, refetch],
  );

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
      const item = connectorItemParams(target.name ?? '');
      const etagQuery = target.etag ? { etag: target.etag } : {};
      const resp = item.space
        ? await apiClient.DELETE(SPACE_CONNECTOR_PATH, {
            params: {
              path: {
                organization: item.organization,
                space: item.space,
                connector: item.connector,
              },
              query: etagQuery,
            },
          })
        : await apiClient.DELETE(CONNECTOR_PATH, {
            params: {
              path: {
                organization: item.organization,
                connector: item.connector,
              },
              query: etagQuery,
            },
          });
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
      dialog,
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
      openCreate,
      openEdit,
      closeDialog,
      submit,
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
