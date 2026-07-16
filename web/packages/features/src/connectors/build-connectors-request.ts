import { orderByParam } from '@pivox/ui/resource-admin';

import { buildConnectorFilter } from '@/connectors/build-connector-filter';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

/** AIP list query params — identical shape client-side and in the SSR loader. */
export interface ConnectorsListQuery {
  filter?: string;
  orderBy?: string;
  pageSize: number;
  pageToken?: string;
}

/**
 * The connectors list request, derived from the list-controls state. A
 * discriminated union so `isSpaceScoped` narrows `pathParams` without casts.
 */
export type ConnectorsListRequest =
  | {
      isSpaceScoped: false;
      pathParams: { organization: string };
      query: ConnectorsListQuery;
    }
  | {
      isSpaceScoped: true;
      pathParams: { organization: string; space: string };
      query: ConnectorsListQuery;
    };

/**
 * Builds the connectors list request (path params + query) from the controls
 * state. The single source of truth for BOTH the client `useQuery` and the SSR
 * prefetch, so their react-query keys can't drift and SSR-primed data is read on
 * hydration instead of silently refetching.
 */
export function buildConnectorsListRequest(
  organization: string,
  value: ListControlsValue,
): ConnectorsListRequest {
  const query: ConnectorsListQuery = {
    filter: buildConnectorFilter(value.filters),
    orderBy: orderByParam(value.sort),
    pageSize: value.pageSize,
    pageToken: value.pageToken,
  };
  if (value.scope) {
    return {
      isSpaceScoped: true,
      pathParams: { organization, space: value.scope },
      query,
    };
  }
  return { isSpaceScoped: false, pathParams: { organization }, query };
}
