/**
 * `@pivox/features/api` — the shared authenticated REST client.
 *
 * Consumed by both web apps (`apps/start`, `apps/electron`). The
 * factory wires a Firebase-token getter into the underlying
 * `@pivox/client`; per-app callers only pick the base URL.
 */

export { createPivoxApiClient } from '@/shared/pivox-api-client';
export type { ApiClient } from '@pivox/client';
