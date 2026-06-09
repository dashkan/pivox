'use client';

import { useEffect, useRef, useState } from 'react';

import { useAuth } from './use-auth';

/**
 * Reads the `pivox_user_id` custom claim off the current Firebase ID
 * token. The claim is set server-side by the Cloud Function
 * `syncIdentity` blocking hook at sign-up and reflected on every
 * subsequent token rotation, so the value is stable for the lifetime
 * of the user's identity row.
 *
 * `initialValue` seeds the state for SSR. The web start app passes the
 * server-verified id from its `_app` route context, so the value is
 * present during SSR and the client's first render (the lazy
 * initializer keeps both in sync — no hydration mismatch), then the
 * effect re-resolves it from the live client claim. Electron has no
 * server session and passes nothing.
 *
 * Returns:
 *   - `undefined` while loading (no seed AND no Firebase user yet)
 *   - `string` once the claim (or seed) is read
 *   - `null` if the user has no `pivox_user_id` claim (e.g. an
 *     account created before the sync-identity hook landed, or one
 *     that failed to provision — the route layer should fall back
 *     to a "complete your account" path)
 *
 * Uses `getIdTokenResult()`, which is cached by the Firebase SDK
 * and only round-trips to the auth server when the token rotates,
 * so calling this from many places is cheap.
 */
export function usePivoxUserId(
  initialValue?: string | null,
): string | null | undefined {
  const { user } = useAuth();
  const [pivoxUserId, setPivoxUserId] = useState<string | null | undefined>(
    initialValue,
  );
  // Tracks whether we've ever resolved a live Firebase user, so the
  // sign-out reset below fires ONLY on an actual sign-out — not on the
  // initial null while Firebase restores its persisted user. Without
  // this, a seeded value (start's SSR id) would flash to `undefined`
  // during the restore window before the claim re-resolves.
  const hadUserRef = useRef(false);

  useEffect(() => {
    if (!user) {
      if (hadUserRef.current) {
        // Reset to "loading" on an actual sign-out so consumers re-gate
        // on auth instead of holding the previous user's UUID (the
        // authenticated tree unmounts on sign-out anyway).
        setPivoxUserId(undefined);
        hadUserRef.current = false;
      }
      return;
    }
    hadUserRef.current = true;
    let cancelled = false;
    void user
      .getIdTokenResult()
      .then((result) => {
        if (cancelled) return;
        const claim = result.claims.pivox_user_id;
        setPivoxUserId(typeof claim === 'string' ? claim : null);
      })
      .catch(() => {
        // Token-fetch failures surface through the AuthInterceptor
        // on subsequent requests; reporting "no pivox user id" here
        // is safer than throwing and crashing the consumer tree.
        if (!cancelled) setPivoxUserId(null);
      });
    return () => {
      cancelled = true;
    };
  }, [user]);

  return pivoxUserId;
}
