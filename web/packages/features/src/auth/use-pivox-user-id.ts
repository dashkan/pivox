'use client';

import { useEffect, useState } from 'react';

import { useAuth } from './use-auth';

/**
 * Reads the `pivox_user_id` custom claim off the current Firebase ID
 * token. The claim is set server-side by the Cloud Function
 * `syncIdentity` blocking hook at sign-up and reflected on every
 * subsequent token rotation, so the value is stable for the lifetime
 * of the user's identity row.
 *
 * Returns:
 *   - `undefined` while loading (no Firebase user yet, or the claim
 *     hasn't been fetched on the first pass)
 *   - `string` once the claim is read
 *   - `null` if the user has no `pivox_user_id` claim (e.g. an
 *     account created before the sync-identity hook landed, or one
 *     that failed to provision — the route layer should fall back
 *     to a "complete your account" path)
 *
 * Uses `getIdTokenResult()`, which is cached by the Firebase SDK
 * and only round-trips to the auth server when the token rotates,
 * so calling this from many places is cheap.
 */
export function usePivoxUserId(): string | null | undefined {
  const { user } = useAuth();
  const [pivoxUserId, setPivoxUserId] = useState<string | null | undefined>(
    undefined,
  );

  useEffect(() => {
    if (!user) {
      // Reset back to "loading" on sign-out so consumers re-gate
      // on auth instead of holding the previous user's UUID. The
      // rule against sync setState in an effect targets cascading
      // renders for derived state — this is the explicit
      // user-changed-to-null reset, where a single render IS the
      // correct outcome (the route unmounts the authenticated
      // tree). Documented escape per the rule's `react.dev/learn/
      // you-might-not-need-an-effect` reference.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setPivoxUserId(undefined);
      return;
    }
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
