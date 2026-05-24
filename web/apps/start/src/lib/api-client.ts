import { createPivoxApiClient } from '@pivox/features/api';

/**
 * Authenticated Pivox REST client for the browser app. Same origin as
 * the cloud (nginx fans `/v1/`, `/internal/`, and the SPA root to the
 * same listener), so `window.location.origin` is the base URL.
 */
export const apiClient = createPivoxApiClient({
  baseUrl: window.location.origin,
});
