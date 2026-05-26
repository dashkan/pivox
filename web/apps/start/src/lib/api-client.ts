import { createReactQueryApi } from '@pivox/client/react-query';
import { createPivoxApiClient } from '@pivox/features/api';

/**
 * Authenticated Pivox REST client for the browser app. Same origin as
 * the cloud (nginx fans `/v1/`, `/internal/`, and the SPA root to the
 * same listener), so `window.location.origin` is the base URL.
 *
 * The `typeof window` guard keeps SSR from crashing on module load
 * (start renders server-side first). The empty fallback never reaches
 * a real call — auth-gated routes wait for Firebase before issuing
 * any API request, and Firebase is also client-only. On the client
 * hydration, the module re-evaluates with `window` defined and the
 * real origin lands in `baseUrl`.
 */
const BASE_URL = typeof window === 'undefined' ? '' : window.location.origin;

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
