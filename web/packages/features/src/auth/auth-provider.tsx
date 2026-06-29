'use client';

import { reportError } from '@pivox/observability';
import {
  getAuth,
  onIdTokenChanged,
  signOut as firebaseSignOut,
} from 'firebase/auth';
import { useCallback, useEffect, useReducer, useRef, useState } from 'react';

import { FirebaseUserContext } from './firebase-user';
import { AuthContext, type AuthUser } from './use-auth';

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

/** Map a Firebase user + resolved Pivox identity id to the neutral shape. */
function toAuthUser(user: User, pivoxUserId: string | null): AuthUser {
  return {
    id: pivoxUserId,
    email: user.email,
    displayName: user.displayName,
    photoURL: user.photoURL,
  };
}

/** Read the `pivox_user_id` claim (the Pivox identity id) off the ID token. */
async function resolvePivoxUserId(user: User): Promise<string | null> {
  try {
    const result = await user.getIdTokenResult();
    const claim = result.claims.pivox_user_id;
    return typeof claim === 'string' ? claim : null;
  } catch {
    // Token-fetch failures surface via the AuthInterceptor on later requests;
    // reporting "no id" here beats crashing the consumer tree.
    return null;
  }
}

/**
 * Firebase auth provider. Supplies TWO contexts:
 *  - `AuthContext` — the platform-neutral `AuthUser` (id = `pivox_user_id`
 *    claim) the shared shell + gates consume.
 *  - `FirebaseUserContext` — the raw reactive Firebase `User` + `refreshUser`,
 *    for Firebase-only account management (Electron). Keycloak has no analog.
 *
 * Only Electron renders this now; the web app uses its own Keycloak provider.
 */
export function AuthProvider({
  children,
  onBeforeSignOut,
  onTokenRefresh,
  onSignedOut,
  onSessionMissing,
}: {
  children: React.ReactNode;
  /**
   * Optional hook fired BEFORE the Firebase JS SDK sign-out runs. Errors are
   * swallowed (logged) — a failed server-side clear shouldn't block sign-out.
   */
  onBeforeSignOut?: () => void | Promise<void>;
  /**
   * Optional hook fired whenever Firebase rotates the ID token. Receives a fresh
   * ID token; skips the initial sign-in fire (the route's onSuccess already
   * minted the session). Errors are swallowed (logged).
   */
  onTokenRefresh?: (idToken: string) => Promise<unknown>;
  /**
   * Optional hook fired AFTER the Firebase JS SDK sign-out completes (e.g. to
   * navigate to the login route).
   */
  onSignedOut?: () => void | Promise<void>;
  /**
   * Optional hook fired ONCE, on the first auth-state resolution, when the
   * Firebase client SDK comes up with NO user — lets the host attempt a silent
   * re-establish from a still-valid server session.
   */
  onSessionMissing?: () => void | Promise<void>;
}) {
  const [firebaseUser, setFirebaseUser] = useState<User | null>(null);
  const [authUser, setAuthUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(true);
  const [, forceUpdate] = useReducer((c: number) => c + 1, 0);
  // Latest-callback refs (avoid resubscribing the listener on parent re-render,
  // which would lose the seen-uid state). Mirrored via effects, not assigned
  // during render.
  const onTokenRefreshRef = useRef(onTokenRefresh);
  useEffect(() => {
    onTokenRefreshRef.current = onTokenRefresh;
  }, [onTokenRefresh]);
  const lastRefreshedUidRef = useRef<string | null>(null);
  const onSessionMissingRef = useRef(onSessionMissing);
  useEffect(() => {
    onSessionMissingRef.current = onSessionMissing;
  }, [onSessionMissing]);
  const initialAuthResolvedRef = useRef(false);
  // The uid the neutral authUser currently reflects — guards the async claim
  // resolution from clobbering a newer user that arrived while it was in flight.
  const currentUidRef = useRef<string | null>(null);

  useEffect(() => {
    const auth = getAuth();
    const unsubscribe = onIdTokenChanged(auth, (fbUser) => {
      setFirebaseUser(fbUser);
      setLoading(false);
      currentUidRef.current = fbUser?.uid ?? null;

      // On the FIRST resolution only: if the client SDK has no user, the server
      // session (cookie) may still be valid — a desync the host can recover.
      if (!initialAuthResolvedRef.current) {
        initialAuthResolvedRef.current = true;
        if (!fbUser) {
          void onSessionMissingRef.current?.();
        }
      }
      if (!fbUser) {
        setAuthUser(null);
        // Signed out — clear the seen-uid memo so a later sign-in starts fresh.
        lastRefreshedUidRef.current = null;
        return;
      }

      // Show display fields immediately; fill the id once the claim resolves
      // (getIdTokenResult is SDK-cached, so this is fast and usually a no-op
      // round-trip). Guard against a newer user arriving mid-flight.
      setAuthUser(toAuthUser(fbUser, null));
      void resolvePivoxUserId(fbUser).then((id) => {
        if (currentUidRef.current === fbUser.uid) {
          setAuthUser(toAuthUser(fbUser, id));
        }
      });

      const cb = onTokenRefreshRef.current;
      if (!cb) return;
      if (lastRefreshedUidRef.current === null) {
        // First fire for this user — sign-in just minted the cookie, or a reload
        // with a still-valid cookie. Nothing to refresh; mark seen.
        lastRefreshedUidRef.current = fbUser.uid;
        return;
      }
      // Token rotation while signed in — slide the cookie window.
      void fbUser
        .getIdToken()
        .then((idToken) => cb(idToken))
        .catch((err: unknown) => {
          reportError(err, { source: 'AuthProvider.onTokenRefresh' });
        });
    });

    return () => {
      unsubscribe();
    };
  }, []);

  const refreshUser = useCallback(async () => {
    const auth = getAuth();
    if (auth.currentUser) {
      // reload() fetches fresh user properties but its internal
      // mergeProviderData() is buggy — patchProviderData() prunes stale
      // providers the server no longer returns.
      await auth.currentUser.reload();
      await patchProviderData(auth.currentUser);
      setFirebaseUser(auth.currentUser);
      const id = await resolvePivoxUserId(auth.currentUser);
      setAuthUser(toAuthUser(auth.currentUser, id));
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
    <AuthContext value={{ user: authUser, loading, signOut }}>
      <FirebaseUserContext value={{ user: firebaseUser, refreshUser }}>
        {children}
      </FirebaseUserContext>
    </AuthContext>
  );
}
