import { createApiClient, type ApiClient } from '@pivox/client';

/**
 * Builds a `@pivox/client` instance pointed at `baseUrl`.
 *
 * Authentication is the transport's responsibility, not this client's: the web
 * app reaches the API through its same-origin `/api/v1` BFF proxy, which attaches
 * the session from an HttpOnly cookie server-side. No bearer token is threaded
 * from the browser, so this factory wires no `getAuthToken` — unauthenticated
 * calls answer 401, which is the right behavior (silently dropping the call would
 * mask bugs).
 *
 * Per-app callers supply only `baseUrl`:
 *   - apps/start: `window.location.origin` (same-origin BFF).
 */
export function createPivoxApiClient(input: { baseUrl: string }): ApiClient {
  return createApiClient({ baseUrl: input.baseUrl });
}
