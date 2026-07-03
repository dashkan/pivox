/**
 * `@pivox/features/api` — the shared Pivox Cloud REST client factory.
 *
 * Consumed by both web apps (`apps/start`, `apps/electron`). The base URL and
 * the token source are per-host: the `start` BFF omits the token (its proxy
 * attaches it from an HttpOnly cookie), while `electron` passes a `getAuthToken`
 * that yields its Keycloak access token. See {@link createPivoxApiClient}.
 */

export { createPivoxApiClient } from '@/shared/pivox-api-client';
export type { ApiClient, AuthTokenGetter } from '@pivox/client';
