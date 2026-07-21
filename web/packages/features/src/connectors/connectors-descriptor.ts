'use client';

import { connectorsListView } from '@pivox/ui/resource-admin';

import { buildConnectorsListRequest } from '@/connectors/build-connectors-request';
import { deleteConnector, saveConnector } from '@/connectors/save-connector';

import type {
  FormDescriptor,
  ListDescriptor,
  ResourceAdmin,
} from '@/resource-admin';
import type { components } from '@pivox/client/types';
import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  ConnectorListExtras,
  SpaceOption,
} from '@pivox/ui/resource-admin';

type ListConnectorsResponse = components['schemas']['v1ListConnectorsResponse'];

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;
const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a new
 * query key (filter / scope / page change) loads — no empty flash, no `isLoading`
 * flip. Equivalent to `keepPreviousData`, inlined to avoid a direct dep on it.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

/** The route-owned slice of the list extras: SSR-prefetched agents + spaces. */
export interface ConnectorListInjected {
  agentOptions: AgentOption[];
  spaceOptions: SpaceOption[];
}

/**
 * The connectors LIST descriptor (data side). The scope switches the list PARENT
 * path; both queries are declared so the hook count stays stable and only the one
 * matching the scope is enabled. Its react-query key is byte-identical to the SSR
 * loader's prime (same literal path + shared `buildConnectorsListRequest`), so
 * primed rows hydrate instead of refetching.
 */
export const connectorsListDescriptor: ListDescriptor<
  Connector,
  ConnectorListExtras,
  ConnectorListInjected,
  ListConnectorsResponse
> = {
  key: 'connectors',
  useList({ $api, organization, state }) {
    // The shared builder produces the exact query the SSR loader keys on.
    const { query } = buildConnectorsListRequest(organization, state);
    const scope = state.scope;

    const orgListQuery = $api.useQuery(
      'get',
      CONNECTORS_PATH,
      { params: { path: { organization }, query } },
      { enabled: scope === '', placeholderData: keepPrevious },
    );
    const spaceListQuery = $api.useQuery(
      'get',
      SPACE_CONNECTORS_PATH,
      { params: { path: { organization, space: scope }, query } },
      { enabled: scope !== '', placeholderData: keepPrevious },
    );
    const listQuery = scope === '' ? orgListQuery : spaceListQuery;
    return {
      data: listQuery.data,
      isLoading: listQuery.isLoading,
      // react-query uses `null` for "no error"; the ApiError arm is `undefined`.
      error: listQuery.error ?? undefined,
      refetch: listQuery.refetch,
    };
  },
  rowsOf: (data) => data?.connectors ?? [],
  nextPageTokenOf: (data) => data?.nextPageToken,
  rowId: (connector) => connector.name ?? '',
  extrasOf: (data, injected) => ({
    agentOptions: injected.agentOptions,
    spaceOptions: injected.spaceOptions,
    // Distinct non-empty agents present in the base scope (server-computed, NOT
    // narrowed by the filter). Sources the agent FILTER facet.
    agentsInUse: data?.agentsInUse ?? [],
  }),
  remove: (apiClient, connector) => deleteConnector({ apiClient, connector }),
  loadErrorFallback: "Couldn't load connectors.",
  view: connectorsListView,
};

/**
 * The connectors FORM descriptor (data side). The single-record detail query is
 * keyed identically to the SSR loader's `setQueryData` (same `$api` params) so
 * the prefetched record hydrates with no XHR on load.
 */
export const connectorsFormDescriptor: FormDescriptor<
  Connector,
  ConnectorFormValues
> = {
  useRecord({ $api, organization, id, space, enabled }) {
    const recordQuery = $api.useQuery(
      'get',
      space ? SPACE_CONNECTOR_PATH : CONNECTOR_PATH,
      {
        params: {
          path: space
            ? { organization, space, connector: id ?? '' }
            : { organization, connector: id ?? '' },
        },
      },
      { enabled, placeholderData: keepPrevious },
    );
    return {
      data: recordQuery.data,
      isLoading: recordQuery.isLoading,
      // react-query uses `null` for "no error"; the ApiError arm is `undefined`.
      error: recordQuery.error ?? undefined,
    };
  },
  save: (input) => saveConnector(input),
  remove: (apiClient, connector) => deleteConnector({ apiClient, connector }),
  loadErrorFallback: "Couldn't load this connector.",
  saveErrorFallback: "Couldn't save the connector.",
};

/** The connectors admin — List + default form create/edit (no custom view). */
export const connectorsResourceAdmin: ResourceAdmin<
  Connector,
  ConnectorFormValues,
  ConnectorListExtras,
  ConnectorListInjected,
  ListConnectorsResponse
> = {
  list: connectorsListDescriptor,
  form: connectorsFormDescriptor,
};
