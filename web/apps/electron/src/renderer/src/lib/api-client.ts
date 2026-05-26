import { createReactQueryApi } from '@pivox/client/react-query';
import { createPivoxApiClient } from '@pivox/features/api';

// Same env var the main process reads; falls back to the dev ngrok
// tunnel when unset. See env.d.ts + electron.vite.config.ts.
const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';

/**
 * Authenticated Pivox REST client for the Electron renderer. Points at
 * the Pivox app origin (the renderer's own `window.location.origin` is
 * a meaningless `file://` or vite-dev URL).
 */
export const apiClient = createPivoxApiClient({ baseUrl: BASE_URL });

/**
 * React Query hooks bound to the Pivox API spec. Consumers do
 * `$api.useQuery('get', '/v1/...', { params: {...} })` and get
 * fully-typed args + response.
 *
 * Requires `<QueryClientProvider client={queryClient}>` somewhere
 * above the consumer — wired in `__root.tsx`.
 */
export const $api = createReactQueryApi(apiClient);
