import { orderByParam } from '@pivox/ui/resource-admin';

import { buildSecretFilter } from '@/secrets/build-secret-filter';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

/** AIP list query params — identical shape client-side and in the SSR loader. */
export interface SecretsListQuery {
  filter?: string;
  orderBy?: string;
  pageSize: number;
  pageToken?: string;
}

/**
 * The secrets list request, derived from the list-controls state. A
 * discriminated union so `isSpaceScoped` narrows `pathParams` without casts —
 * the secret twin of `ConnectorsListRequest`.
 */
export type SecretsListRequest =
  | {
      isSpaceScoped: false;
      pathParams: { organization: string };
      query: SecretsListQuery;
    }
  | {
      isSpaceScoped: true;
      pathParams: { organization: string; space: string };
      query: SecretsListQuery;
    };

/**
 * Builds the secrets list request (path params + query) from the controls state.
 * The single source of truth for BOTH the client `useQuery` and the SSR prefetch,
 * so their react-query keys can't drift and SSR-primed data is read on hydration
 * instead of silently refetching.
 */
export function buildSecretsListRequest(
  organization: string,
  value: ListControlsValue,
): SecretsListRequest {
  const query: SecretsListQuery = {
    filter: buildSecretFilter(value.filters),
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
