/**
 * Server-side Pivox API client for SSR data prefetch.
 *
 * The browser uses `apps/start/src/lib/api-client.ts` to call the
 * backend with Firebase ID tokens. The SSR server can't reach the
 * Firebase JS SDK and needs a different auth path: it mints
 * SA-signed JWTs via `pivox-actor-token.ts` and uses them as Bearer
 * when calling the backend on behalf of an authenticated user.
 *
 * This module wires those pieces:
 *  - Reads env-var config (SA email + audience + backend URL)
 *  - Lazily constructs a process-singleton actor token source
 *  - Builds per-request `apiClient` + react-query bindings keyed on
 *    the user's Pivox UUID
 *
 * Per-request because the actor token is per-user — handlers in
 * SSR's `beforeLoad` call `createServerApi(session.user.pivoxUserId)`,
 * use the result for `queryClient.prefetchQuery(...)`, then drop it.
 * The underlying actor token source caches per uid across requests
 * so successive prefetches for the same user share one mint.
 *
 * SSR-only. Importing this from client code is a bug (it pulls in
 * `google-auth-library`, which is bundling-hostile per the Nitro
 * externals config).
 */

import { createApiClient } from '@pivox/client';
import { createReactQueryApi, type ReactQueryApi } from '@pivox/client/react-query';

import {
  createActorTokenSource,
  createGcpActorTokenMint,
  type ActorTokenSource,
} from './pivox-actor-token';

/**
 * SSR backend URL — the address the SSR Node process uses to reach
 * the Pivox API. Typically the same public URL the browser uses
 * when both are behind the same edge proxy (nginx fans `/v1/*` to
 * pivox-cloud). Operators MAY point this at a different internal
 * endpoint for SSR-only deployments (e.g., a private VPC URL).
 */
const ENV_API_URL = 'PIVOX_API_URL';

/**
 * SSR server's own service-account email. The Pivox backend's
 * `PIVOX_SSR_ALLOWED_SERVICE_ACCOUNTS` must include this value or
 * every minted JWT will be rejected at the issuer-allowlist check.
 */
const ENV_SA_EMAIL = 'PIVOX_SSR_SA_EMAIL';

/**
 * Audience expected on minted SA-signed JWTs. Mirrors the backend's
 * `PIVOX_SSR_AUDIENCE` (which itself defaults to `PIVOX_AUDIENCE`
 * for deployments that target one backend URL).
 */
const ENV_AUDIENCE = 'PIVOX_SSR_AUDIENCE';

/**
 * Cached server-API state. Built on first use so module import
 * doesn't trigger GoogleAuth construction — useful for tests that
 * import this file but never call its functions.
 *
 * All three env-driven knobs (token source, API URL) cache together
 * so behavior is consistent: once validated on the first request,
 * later calls reuse the same values even if process.env mutates.
 * Inconsistent caching across knobs would give the illusion of
 * runtime reconfiguration without delivering it (the singleton
 * token source is already locked to its SA+audience).
 */
interface CachedConfig {
  tokenSource: ActorTokenSource;
  baseUrl: string;
}

let _cached: CachedConfig | null = null;

function getConfig(): CachedConfig {
  if (_cached) return _cached;
  const saEmail = process.env[ENV_SA_EMAIL];
  const audience = process.env[ENV_AUDIENCE];
  const baseUrl = process.env[ENV_API_URL];
  if (!baseUrl) {
    throw new Error(
      `${ENV_API_URL} not set; SSR server cannot reach the Pivox API. ` +
        `Set it to the backend's public URL (e.g., https://api.pivox.app).`,
    );
  }
  if (!saEmail) {
    throw new Error(
      `${ENV_SA_EMAIL} not set; SSR server cannot mint actor tokens. ` +
        `Set it to the email of the service account hosting the SSR process.`,
    );
  }
  if (!audience) {
    throw new Error(
      `${ENV_AUDIENCE} not set; SSR server cannot mint actor tokens. ` +
        `Set it to the backend's expected JWT audience (typically the API URL).`,
    );
  }
  _cached = {
    tokenSource: createActorTokenSource(
      createGcpActorTokenMint({ serviceAccountEmail: saEmail, audience }),
    ),
    baseUrl,
  };
  return _cached;
}

/**
 * Resets the cached server-api state. Tests reach for this to
 * exercise env-var validation paths without leaking cached state
 * across test cases. Not for production use.
 *
 * @internal
 */
export function _resetServerApiForTests(): void {
  _cached = null;
}

/**
 * createServerApi builds a typed Pivox API client + react-query
 * bindings that authenticate as the given user via an SA-signed
 * actor JWT. The returned ReactQueryApi has the same shape as the
 * client-side `$api` (from `apps/start/src/lib/api-client.ts`), so
 * `queryOptions(...)` produces queryKeys that match the client's
 * `useQuery` calls — SSR-prefetched data hydrates the client's
 * QueryClient cache without explicit handoff.
 *
 * Usage in `beforeLoad`:
 *   const session = await getServerSession();
 *   if (!session.user?.pivoxUserId) throw redirect({ to: '/auth/login' });
 *   const api = createServerApi(session.user.pivoxUserId);
 *   await context.queryClient.prefetchQuery(
 *     api.queryOptions('get', '/v1/accounts/me/organizations', { ... }),
 *   );
 *
 * Throws on missing env-var config — the SSR server can't proceed
 * without a way to mint tokens, and silently degrading would mean
 * every API call fails at the gateway instead of at boot.
 */
export function createServerApi(pivoxUserId: string): ReactQueryApi {
  if (!pivoxUserId) {
    throw new Error(
      'createServerApi: pivoxUserId is required. The Firebase blocking ' +
        'function may not have fired yet — surface as a recoverable error ' +
        'and refresh the ID token.',
    );
  }
  const cfg = getConfig();
  const apiClient = createApiClient({
    baseUrl: cfg.baseUrl,
    getAuthToken: () => cfg.tokenSource(pivoxUserId),
  });
  return createReactQueryApi(apiClient);
}
