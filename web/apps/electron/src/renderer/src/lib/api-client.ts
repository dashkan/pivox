import { createReactQueryApi } from '@pivox/client/react-query';
import { createPivoxApiClient } from '@pivox/features/api';

// Same env var the main process reads; falls back to the local dev
// origin (localhost:8081) when unset. See env.d.ts + electron.vite.config.ts.
const BASE_URL = import.meta.env.VITE_BASE_URL || 'http://localhost:8081';

/**
 * Authenticated Pivox REST client for the Electron renderer. Points at the Pivox
 * app origin (the renderer's own `window.location.origin` is a meaningless
 * `file://` or vite-dev URL). Unlike the web app's same-origin BFF proxy, the
 * renderer attaches the bearer itself: `getAuthToken` fetches the Keycloak access
 * token over IPC from the main process (which holds + refreshes it).
 *
 * When signed out (or a refresh just failed), the IPC call rejects; we map that
 * to `null` so the request goes out unauthenticated and the server answers a
 * normal `401` — matching the BFF path's error shape (a raw IPC rejection would
 * diverge from it and trigger pointless React Query retries). A failed refresh
 * also signs the session out in main, so the gate redirects to /auth/login.
 */
export const apiClient = createPivoxApiClient({
  baseUrl: BASE_URL,
  getAuthToken: async () => {
    try {
      return await window.api.getAccessToken();
    } catch {
      return null;
    }
  },
});

/**
 * React Query hooks bound to the Pivox API spec. Consumers do
 * `$api.useQuery('get', '/v1/...', { params: {...} })` and get
 * fully-typed args + response.
 *
 * Requires `<QueryClientProvider client={queryClient}>` somewhere
 * above the consumer — wired in `__root.tsx`.
 */
export const $api = createReactQueryApi(apiClient);
