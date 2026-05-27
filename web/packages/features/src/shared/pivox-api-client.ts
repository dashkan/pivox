import { createApiClient, type ApiClient } from '@pivox/client';
import { getAuth, onAuthStateChanged } from 'firebase/auth';

/**
 * Process-singleton Promise that resolves when Firebase fires its
 * first auth-state event — either a signed-in user loaded from
 * IndexedDB or a confirmed signed-out state. Cached so successive
 * requests after the first event share zero overhead.
 *
 * Why: Firebase's JS SDK loads the persisted user from IndexedDB
 * asynchronously after `getAuth()` returns. For ~50–200ms after SDK
 * init, `getAuth().currentUser` is `null` even though the user is
 * signed in. Naïve `getIdToken()` callers race that window — the
 * first authenticated request goes out with no Authorization header,
 * the gateway returns 401, and React Query's default retry kicks in.
 * The retry succeeds because Firebase has loaded by then. The 401 is
 * harmless but wastes an RPC and pollutes server logs.
 *
 * Gating `getAuthToken` on the first `onAuthStateChanged` event
 * removes the race at the source: every authenticated call waits the
 * sub-second-once-per-tab cost, then proceeds with the correct user.
 */
let _authReady: Promise<void> | null = null;

function waitForAuthReady(): Promise<void> {
  if (_authReady) return _authReady;
  _authReady = new Promise<void>((resolve) => {
    const unsubscribe = onAuthStateChanged(getAuth(), () => {
      // First event resolves the gate; unsubscribe so subsequent
      // sign-in / sign-out events don't try to re-resolve.
      unsubscribe();
      resolve();
    });
  });
  return _authReady;
}

/**
 * Internal test hook. Tests that exercise the auth-ready gate clear
 * the singleton between cases so each test owns its own listener
 * lifecycle. Not for production use.
 *
 * @internal
 */
export function __resetAuthReadyForTests(): void {
  _authReady = null;
}

/**
 * Builds an authenticated `@pivox/client` instance pointed at
 * `baseUrl`. The auth token comes from the active Firebase user via
 * `getIdToken()` — called fresh per request, so Firebase's own near-
 * expiry refresh applies transparently. When no user is signed in,
 * the call goes out unauthenticated and the gateway answers 401, which
 * is the right behavior (silently dropping the call would mask bugs).
 *
 * Every request awaits the first `onAuthStateChanged` event before
 * reading `currentUser`. See `waitForAuthReady` above for the race
 * this closes.
 *
 * Per-app callers supply only `baseUrl`:
 *   - apps/start: `window.location.origin` (served same-origin via
 *     nginx with the cloud).
 *   - apps/electron renderer: `import.meta.env.VITE_BASE_URL`.
 */
export function createPivoxApiClient(input: { baseUrl: string }): ApiClient {
  return createApiClient({
    baseUrl: input.baseUrl,
    getAuthToken: async () => {
      await waitForAuthReady();
      return getAuth().currentUser?.getIdToken();
    },
  });
}
