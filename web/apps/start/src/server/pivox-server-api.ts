/**
 * Server-side Pivox API client for SSR data prefetch.
 *
 * The browser calls the backend through the BFF proxy (`/api/v1/*`), which
 * injects the Keycloak access token from the httpOnly session cookie. The SSR
 * server can't go through that proxy (it IS the server), so it calls the
 * backend directly and forwards the SAME user access token as the Bearer —
 * resolved from the session cookie by `oidc/ssr-token.ts`. The cloud backend
 * verifies Keycloak access tokens natively, so this is the user acting as
 * themselves, not a service-account impersonation.
 *
 * SSR-only. The browser never imports this; client data flows through
 * `lib/api-client.ts` → the proxy.
 */

import { createApiClient, type ApiClient } from '@pivox/client';
import {
  createReactQueryApi,
  type ReactQueryApi,
} from '@pivox/client/react-query';

/**
 * SSR backend URL — the address the SSR Node process uses to reach the Pivox
 * API. Same value the BFF proxy forwards to; typically the public API URL.
 */
const ENV_API_URL = 'PIVOX_API_URL';

function backendBaseUrl(): string {
  const baseUrl = process.env[ENV_API_URL];
  if (!baseUrl) {
    throw new Error(
      `${ENV_API_URL} not set; SSR server cannot reach the Pivox API. ` +
        `Set it to the backend's public URL (e.g., https://api.pivox.app).`,
    );
  }
  return baseUrl;
}

/**
 * createServerApiClient builds the openapi-fetch client used for direct
 * (non-react-query) server-side calls. Server functions that fetch on behalf of
 * a user and hand the result to `queryClient.setQueryData(...)` use this
 * directly. The `accessToken` is the user's Keycloak access token, read from the
 * session cookie via `getSsrAccessToken()`.
 */
export function createServerApiClient(accessToken: string): ApiClient {
  if (!accessToken) {
    throw new Error('createServerApiClient: accessToken is required');
  }
  return createApiClient({
    baseUrl: backendBaseUrl(),
    getAuthToken: () => Promise.resolve(accessToken),
  });
}

/**
 * createServerApi builds the react-query-bound variant. Same auth + base URL as
 * `createServerApiClient`; just exposes the higher layer.
 */
export function createServerApi(accessToken: string): ReactQueryApi {
  return createReactQueryApi(createServerApiClient(accessToken));
}
