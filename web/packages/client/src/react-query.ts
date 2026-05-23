import createReactQueryHooks from 'openapi-react-query';

import type { ApiClient } from '@/client';

/**
 * Binds an `@pivox/client` instance to openapi-react-query so consumers
 * get `$api.useQuery(method, path, init)` and `$api.useMutation(...)`
 * typed against the Pivox spec.
 *
 * Usage:
 *
 * ```ts
 * import { createApiClient } from '@pivox/client';
 * import { createReactQueryApi } from '@pivox/client/react-query';
 *
 * const client = createApiClient({ baseUrl, getAuthToken });
 * export const $api = createReactQueryApi(client);
 *
 * // in a component:
 * const { data } = $api.useQuery(
 *   'get',
 *   '/v1/organizations/{organization}',
 *   { params: { path: { organization: 'orgs/acme' } } },
 * );
 * ```
 *
 * Why a thin wrapper instead of re-exporting `createClient` from
 * openapi-react-query? Two reasons: (1) the upstream symbol is named
 * `createClient`, which collides with `createApiClient` from this
 * package in callers that import both; (2) wrapping pins the
 * type-argument inference to our `ApiClient` so downstream callers
 * never have to spell the `paths` generic themselves.
 */
export function createReactQueryApi(client: ApiClient) {
  return createReactQueryHooks(client);
}

export type ReactQueryApi = ReturnType<typeof createReactQueryApi>;
