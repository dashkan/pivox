'use client';

import { createContext, use } from 'react';

import type { User } from 'firebase/auth';

/**
 * The reactive raw Firebase `User` + a refresh, for Firebase-only account
 * management (MFA/TOTP, provider linking, email/password change, delete-account,
 * `providerData`). Supplied by the Firebase `AuthProvider`; consumed by the
 * account UI (`useUserProfile`, verify-email).
 *
 * Keycloak has NO analog — its account console owns account management — so the
 * Keycloak provider does not supply this context. It exists purely so that the
 * shared `AuthContext` can stay platform-neutral ({@link AuthUser}) while
 * Electron's still-Firebase account UI keeps the reactive Firebase user it needs.
 * It is deleted when Electron migrates to Keycloak (account mgmt → KC console).
 */
export interface FirebaseUserContextValue {
  user: User | null;
  refreshUser: () => Promise<void>;
}

export const FirebaseUserContext =
  createContext<FirebaseUserContextValue | null>(null);

export function useFirebaseUser(): FirebaseUserContextValue {
  const ctx = use(FirebaseUserContext);
  if (!ctx) {
    throw new Error(
      'useFirebaseUser must be used within the Firebase AuthProvider',
    );
  }
  return ctx;
}
