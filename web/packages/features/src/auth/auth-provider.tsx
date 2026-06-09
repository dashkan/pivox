'use client';

import { reportError } from '@pivox/observability';
import {
  getAuth,
  onIdTokenChanged,
  signOut as firebaseSignOut,
} from 'firebase/auth';
import { useCallback, useEffect, useReducer, useRef, useState } from 'react';

import { AuthContext } from './use-auth';

import type { User } from 'firebase/auth';

/**
 * Workaround for Firebase SDK bug: `user.reload()` calls an internal
 * `mergeProviderData()` that merges old + new providers instead of replacing.
 * Unlinked providers are never removed because they're absent from the server
 * response and survive the merge. This function patches providerData after
 * reload() by fetching the truth from the REST API and replacing the stale array.
 *
 * See: `mergeProviderData()` in @firebase/auth — line ~1545 of the ESM bundle.
 */
async function patchProviderData(user: User): Promise<void> {
  const idToken = await user.getIdToken();
  const auth = user as unknown as {
    auth: {
      config: { apiKey: string };
      emulatorConfig?: { host: string; port: number; protocol: string };
    };
  };
  const apiKey = auth.auth.config.apiKey;
  const emu = auth.auth.emulatorConfig;
  const baseUrl = emu
    ? `${emu.protocol}://${emu.host}:${emu.port}/identitytoolkit.googleapis.com`
    : 'https://identitytoolkit.googleapis.com';
  const res = await fetch(`${baseUrl}/v1/accounts:lookup?key=${apiKey}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ idToken }),
  });
  if (!res.ok) return;
  // Shape from Firebase Identity Toolkit accounts:lookup.
  const data = (await res.json()) as {
    users?: Array<{
      providerUserInfo?: Array<{
        providerId: string;
        displayName?: string;
        email?: string;
        photoUrl?: string;
        rawId?: string;
      }>;
    }>;
  };
  const serverProviders = data.users?.[0]?.providerUserInfo;
  if (!Array.isArray(serverProviders)) return;

  // Build the correct provider list from the server response.
  const serverProviderIds = new Set(serverProviders.map((p) => p.providerId));

  // Mutate the user's providerData in place — remove providers the server
  // doesn't have, preserving the SDK's internal array reference.
  const pd = user.providerData;
  for (let i = pd.length - 1; i >= 0; i--) {
    const entry = pd[i];
    if (entry && !serverProviderIds.has(entry.providerId)) {
      pd.splice(i, 1);
    }
  }
}

export function AuthProvider({
  children,
  onBeforeSignOut,
  onTokenRefresh,
  onSignedOut,
  onSessionMissing,
}: {
  children: React.ReactNode;
  /**
   * Optional hook fired BEFORE the Firebase JS SDK sign-out runs.
   * In the start app this is wired to `clearSession` so the server-
   * side cookie is cleared (and refresh tokens revoked) before the
   * client tears down its auth state. Electron leaves this unset —
   * no server-side cookie to clear.
   *
   * Errors are swallowed (logged to observability): a server-side
   * clear that fails shouldn't block the client sign-out + redirect.
   * Worst case is a stale cookie that fails verification on the next
   * request — the user is still signed out everywhere they care.
   */
  onBeforeSignOut?: () => void | Promise<void>;
  /**
   * Optional hook fired whenever Firebase rotates the ID token —
   * after sign-in, every ~55 min while the app is open, and on
   * explicit `getIdToken(true)`. Receives a fresh ID token. In the
   * start app this is wired to `createSession` so the server-side
   * cookie's 14-day window slides forward continuously while the
   * user is active — an actively-used app never sees cookie
   * expiry, only inactivity beyond the window does.
   *
   * Errors are swallowed (logged to observability): a single failed
   * refresh isn't fatal — the cookie remains valid until its current
   * expiry, by which point another rotation should succeed.
   *
   * Skips invocation during the initial sign-in path — the route's
   * onSuccess already called `createSession` synchronously, so the
   * first `onIdTokenChanged` fire would just be a wasted round-trip.
   * Detected via a ref that tracks whether the listener has seen
   * this user before.
   */
  onTokenRefresh?: (idToken: string) => Promise<unknown>;
  /**
   * Optional hook fired AFTER the Firebase JS SDK sign-out completes.
   * In the start app this navigates to /auth/login — `_app`'s
   * `beforeLoad` only runs on navigation events, so without an
   * explicit redirect the user would stay on the current route with
   * a null user and the SSR-rendered content still on screen.
   *
   * Electron leaves this unset: its `_app.tsx` still wraps with
   * AuthGateFeature, which catches the null-user state via
   * `<Navigate />` on the next render.
   */
  onSignedOut?: () => void | Promise<void>;
  /**
   * Optional hook fired ONCE, on the first auth-state resolution, when
   * the Firebase client SDK comes up with NO user. In the start app a
   * valid server session cookie can outlive the client's Firebase
   * session (e.g. the auth IndexedDB was evicted) — the cookie gate
   * still renders the app, but CSR calls go out tokenless and 401.
   * This fires so the host can attempt a silent re-establish
   * (cookie → custom token → signInWithCustomToken) and fall back to a
   * real re-login if there's no recoverable server session.
   *
   * Fires only on the INITIAL resolution, never on later transitions —
   * so an explicit sign-out (which also drives the listener to null) is
   * handled by `onSignedOut`, not here. Electron leaves this unset; its
   * AuthGateFeature redirects on a null user instead.
   */
  onSessionMissing?: () => void | Promise<void>;
}) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [, forceUpdate] = useReducer((c: number) => c + 1, 0);
  // Refs live across renders without re-firing the firebase-auth
  // import effect. `onTokenRefreshRef` lets the listener pick up the
  // latest callback without resubscribing on each parent re-render
  // (resubscribing would lose the seen-uid state below).
  // `lastRefreshedUidRef` records which user we've already minted a
  // session for — the first onIdTokenChanged fire for any given user
  // happens right after sign-in, where the route's onSuccess already
  // called createSession. Skipping that fire avoids a wasted
  // round-trip on every sign-in.
  const onTokenRefreshRef = useRef(onTokenRefresh);
  // Mirror the latest callback into the ref via an effect, not a
  // ref-assignment during render — react-hooks/refs flags the
  // latter and the effect form is the canonical "always-current
  // callback" pattern (see React docs on `useEffectEvent`-shaped
  // workarounds prior to its stable release).
  useEffect(() => {
    onTokenRefreshRef.current = onTokenRefresh;
  }, [onTokenRefresh]);
  const lastRefreshedUidRef = useRef<string | null>(null);
  // Latest-ref for onSessionMissing (same always-current-callback
  // pattern as onTokenRefreshRef) plus a one-shot guard so recovery
  // fires only on the FIRST auth-state resolution, never on later
  // null transitions (explicit sign-out is onSignedOut's job).
  const onSessionMissingRef = useRef(onSessionMissing);
  useEffect(() => {
    onSessionMissingRef.current = onSessionMissing;
  }, [onSessionMissing]);
  const initialAuthResolvedRef = useRef(false);

  useEffect(() => {
    // firebase/auth is now a static import (was previously dynamic to
    // try to code-split). The dynamic import was ineffective: every
    // use-* hook in this package statically imports from firebase/auth,
    // so the module landed in the main bundle anyway and the dynamic
    // wrapper just added a microtask + unnecessary error-handling
    // surface. Code-splitting Firebase out of the main bundle is now
    // handled at the bundler level via apps/start's manualChunks.
    const auth = getAuth();
    const unsubscribe = onIdTokenChanged(auth, (firebaseUser) => {
      setUser(firebaseUser);
      setLoading(false);

      // On the FIRST auth-state resolution only: if the client SDK has
      // no user, the server session (cookie) may still be valid — a
      // desync the host can recover silently. Guarded one-shot so later
      // null transitions (explicit sign-out) fall through to the
      // onSignedOut path below instead of triggering recovery.
      if (!initialAuthResolvedRef.current) {
        initialAuthResolvedRef.current = true;
        if (!firebaseUser) {
          void onSessionMissingRef.current?.();
        }
      }
      if (!firebaseUser) {
        // Signed out — clear the seen-uid memo so a subsequent
        // sign-in starts fresh (route's onSuccess will mint the
        // first cookie; subsequent rotations will refresh it).
        lastRefreshedUidRef.current = null;
        return;
      }
      const cb = onTokenRefreshRef.current;
      if (!cb) return;
      if (lastRefreshedUidRef.current === null) {
        // First fire for this user. Either:
        //   - Sign-in just completed and the route's `onSuccess`
        //     already minted the cookie, OR
        //   - Page reload with a persisted user, where the cookie
        //     is still valid (otherwise we wouldn't have gotten
        //     past `beforeLoad`).
        // Both cases mean there's nothing to refresh right now;
        // mark seen and let subsequent rotations slide the
        // window forward.
        lastRefreshedUidRef.current = firebaseUser.uid;
        return;
      }
      // Token rotation while signed in — slide the cookie window.
      void firebaseUser
        .getIdToken()
        .then((idToken) => cb(idToken))
        .catch((err: unknown) => {
          reportError(err, {
            source: 'AuthProvider.onTokenRefresh',
          });
        });
    });

    return () => {
      unsubscribe();
    };
  }, []);

  const refreshUser = useCallback(async () => {
    const auth = getAuth();
    if (auth.currentUser) {
      // reload() fetches fresh user properties (displayName, emailVerified, etc.)
      // but its internal mergeProviderData() is buggy — it never removes
      // unlinked providers. patchProviderData() fixes this by pruning
      // providers that the server no longer returns.
      await auth.currentUser.reload();
      await patchProviderData(auth.currentUser);
      setUser(auth.currentUser);
      forceUpdate();
    }
  }, []);

  const signOut = useCallback(async () => {
    if (onBeforeSignOut) {
      try {
        await onBeforeSignOut();
      } catch (err) {
        reportError(err, { source: 'AuthProvider.onBeforeSignOut' });
      }
    }
    const auth = getAuth();
    await firebaseSignOut(auth);
    if (onSignedOut) {
      try {
        await onSignedOut();
      } catch (err) {
        reportError(err, { source: 'AuthProvider.onSignedOut' });
      }
    }
  }, [onBeforeSignOut, onSignedOut]);

  return (
    <AuthContext value={{ user, loading, signOut, refreshUser }}>
      {children}
    </AuthContext>
  );
}
