import { createApiClient, type ApiClient } from '@pivox/client';
import { getAuth } from 'firebase/auth';

/**
 * Builds an authenticated `@pivox/client` instance pointed at
 * `baseUrl`. The auth token comes from the active Firebase user via
 * `getIdToken()` — called fresh per request, so Firebase's own near-
 * expiry refresh applies transparently. When no user is signed in,
 * the call goes out unauthenticated and the gateway answers 401, which
 * is the right behavior (silently dropping the call would mask bugs).
 *
 * Per-app callers supply only `baseUrl`:
 *   - apps/start: `window.location.origin` (served same-origin via
 *     nginx with the cloud).
 *   - apps/electron renderer: `import.meta.env.VITE_BASE_URL`.
 */
export function createPivoxApiClient(input: { baseUrl: string }): ApiClient {
  return createApiClient({
    baseUrl: input.baseUrl,
    getAuthToken: () => getAuth().currentUser?.getIdToken(),
  });
}
