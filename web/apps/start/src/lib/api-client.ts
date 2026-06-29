import { createApiClient } from '@pivox/client';
import { createReactQueryApi } from '@pivox/client/react-query';

/**
 * Pivox REST client for the browser app. Requests go to the `start` BFF proxy at
 * `/api/v1/*` (same origin), which forwards to the cloud `/v1/*` and injects the
 * Keycloak access token from the httpOnly session cookie. So the browser sends
 * NO bearer of its own — the cookie carries auth and the BFF owns the token +
 * transparent refresh. (Electron keeps using `createPivoxApiClient` with a
 * bearer until its own Keycloak migration; this app no longer needs it.)
 *
 * baseUrl is `<origin>/api`; the generated OpenAPI paths are `/v1/...`, so a call
 * lands on `/api/v1/...` and hits the proxy. The `typeof window` guard keeps SSR
 * from crashing on module load; the empty base is never used for a real call
 * (server-side fetching goes through the SSR path, not this browser client).
 */
const BASE_URL = typeof window === 'undefined' ? '' : `${window.location.origin}/api`;

export const apiClient = createApiClient({ baseUrl: BASE_URL });

/**
 * React Query hooks bound to the Pivox API spec. Consumers do
 * `$api.useQuery('get', '/v1/...', { params: {...} })` and get fully-typed
 * args + response. Requires `<QueryClientProvider>` above the consumer
 * (wired in `__root.tsx`).
 */
export const $api = createReactQueryApi(apiClient);
