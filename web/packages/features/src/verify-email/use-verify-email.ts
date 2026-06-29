'use client';

import { sendEmailVerification } from 'firebase/auth';
import { useState } from 'react';

import type {
  VerifyEmailContextValue,
  VerifyEmailState,
} from '@pivox/ui/verify-email-card';

import { useFirebaseUser } from '@/auth/firebase-user';
import { firebaseErrorMessage } from '@/shared/firebase-error';

/**
 * Drives the verify-email informational screen.
 *
 * Verification itself is handled deterministically by `/auth/action`
 * — the route Firebase's verification link opens. That route calls
 * `applyActionCode`, refreshes the cached user, and navigates the
 * user into the app. This hook is purely a "we sent you a link"
 * screen plus a Resend button — no polling, no auto-advance.
 *
 * If the user clicks the verification link in a different device or
 * browser, the original tab will stay on this screen until they
 * refresh or sign in again. That's an accepted tradeoff for keeping
 * the verification path deterministic instead of relying on
 * client-side polling.
 */
export function useVerifyEmail(): VerifyEmailContextValue {
  // Read the user from the AuthProvider context instead of calling
  // `getAuth()` at render — Firebase is client-only (initializeApp
  // is guarded by `typeof window`), and `getAuth()` during SSR
  // throws "No Firebase App '[DEFAULT]'".
  // Firebase-only (Electron): email verification is handled by Keycloak's
  // account console on the web. Reads the raw Firebase user.
  const { user } = useFirebaseUser();
  const [resent, setResent] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const state: VerifyEmailState = {
    email: user?.email ?? null,
    resent,
    error,
  };

  const actions = {
    resendVerification: async () => {
      setError(null);
      setResent(false);
      try {
        if (!user) {
          setError('No user signed in');
          return;
        }
        await sendEmailVerification(user);
        setResent(true);
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },
  };

  return { state, actions };
}
