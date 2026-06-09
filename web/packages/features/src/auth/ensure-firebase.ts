'use client';

import { getApps, initializeApp, type FirebaseOptions } from 'firebase/app';
import {
  browserLocalPersistence,
  indexedDBLocalPersistence,
  initializeAuth,
} from 'firebase/auth';

/**
 * Initialize the Firebase client app + Auth once, with explicit,
 * hardened persistence. Shared by every Pivox renderer (web start,
 * electron) so the auth-persistence setup can't drift between them —
 * each app passes only its own `config` (env-derived).
 *
 * Why `initializeAuth` instead of letting the first `getAuth()` pick a
 * default: with the default, if IndexedDB isn't reachable at that first
 * call the SDK silently locks to in-memory persistence for the session,
 * and the user is lost on every reload while the server cookie lingers
 * — the half-auth desync we hit in start. Pinning the persistence here
 * (IndexedDB, then localStorage) keeps that from happening on any app.
 * `navigator.storage.persist()` additionally asks the browser not to
 * evict that store under pressure.
 *
 * Idempotent (guards on `getApps()`), and a no-op during SSR (the
 * `typeof window` guard) so the web start server doesn't touch it.
 * Callers wrap this in a per-app `ensureFirebase()` that holds the
 * config and is invoked at router construction, ahead of any
 * `getAuth()` consumer — so `initializeAuth` always wins.
 */
export function ensureFirebaseApp(config: FirebaseOptions): void {
  if (typeof window === 'undefined') return;
  if (getApps().length > 0) return;

  const app = initializeApp(config);
  initializeAuth(app, {
    persistence: [indexedDBLocalPersistence, browserLocalPersistence],
  });

  // `navigator.storage` / `.persist` are typed as always-present but are
  // absent in non-secure contexts and older browsers, so the runtime
  // guard is real despite the type saying it's unnecessary.
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition
  void navigator.storage?.persist?.();
}
