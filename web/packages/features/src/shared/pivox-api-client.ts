import {
  createApiClient,
  type ApiClient,
  type AuthTokenGetter,
} from '@pivox/client';

/**
 * Builds a `@pivox/client` instance pointed at `baseUrl`, optionally wiring a
 * bearer-token source.
 *
 * How the token reaches the API differs per host, and this seam carries both:
 *   - apps/start (BFF): omit `getAuthToken`. The browser reaches the API through
 *     a same-origin `/api/v1` proxy that attaches the session from an HttpOnly
 *     cookie server-side — no bearer is threaded from the client. `baseUrl` is
 *     `window.location.origin`.
 *   - apps/electron (no BFF): pass `getAuthToken`. The renderer has no cookie
 *     session, so it sources a Keycloak access token (over IPC from the main
 *     process, which holds and refreshes it) and this attaches it as
 *     `Authorization: Bearer`. `baseUrl` is the remote API origin.
 *
 * With no getter, unauthenticated calls answer 401 — the right behavior for the
 * BFF path (silently dropping the call would mask bugs).
 */
export function createPivoxApiClient(input: {
  baseUrl: string;
  getAuthToken?: AuthTokenGetter;
}): ApiClient {
  return createApiClient({
    baseUrl: input.baseUrl,
    ...(input.getAuthToken ? { getAuthToken: input.getAuthToken } : {}),
  });
}
