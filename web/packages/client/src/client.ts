import createOpenApiClient, { type Middleware } from 'openapi-fetch';

import type { paths } from '@/generated/types.gen';

/**
 * Returns the current bearer token, or null/undefined if the caller is
 * unauthenticated. May be sync or async — the auth middleware awaits the
 * return value before each request, so refreshing tokens (e.g. Firebase
 * `getIdToken(forceRefresh)`) happens transparently.
 */
export type AuthTokenGetter = () =>
  | Promise<string | null | undefined>
  | string
  | null
  | undefined;

export type ApiClientConfig = {
  /** Pivox Cloud REST gateway base URL. */
  baseUrl: string;
  /** Optional bearer-token source. Omit for un-authed clients. */
  getAuthToken?: AuthTokenGetter;
  /**
   * Overrides the global `fetch`. Production callers leave this unset.
   * Tests pass a recording stub to assert on outgoing requests.
   */
  fetch?: typeof globalThis.fetch;
};

export type ApiClient = ReturnType<typeof createApiClient>;

export function createApiClient(config: ApiClientConfig) {
  const client = createOpenApiClient<paths>({
    baseUrl: config.baseUrl,
    fetch: config.fetch,
  });

  if (config.getAuthToken) {
    client.use(authMiddleware(config.getAuthToken));
  }

  return client;
}

function authMiddleware(getAuthToken: AuthTokenGetter): Middleware {
  return {
    async onRequest({ request }) {
      const token = await getAuthToken();
      if (token) {
        request.headers.set('Authorization', `Bearer ${token}`);
      }
      return request;
    },
  };
}
