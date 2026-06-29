'use client';

import { createContext, use } from 'react';

/**
 * Platform-neutral authenticated user for the app shell + route gates.
 *
 * `id` is the Pivox identity id — the Keycloak `sub` (which equals
 * `identities.id`) for the web BFF, or the `pivox_user_id` claim for Firebase
 * (Electron) — so it means the same thing on both platforms. `null` id means the
 * identity hasn't been provisioned yet (Firebase: claim missing).
 *
 * This is deliberately free of any Firebase types: the web app is Keycloak-only,
 * and Electron's provider maps its Firebase user into this shape. Firebase-only
 * account management reads the raw user via `useFirebaseUser` instead.
 */
export interface AuthUser {
  id: string | null;
  email: string | null;
  displayName: string | null;
  photoURL: string | null;
}

export interface AuthContextValue {
  user: AuthUser | null;
  loading: boolean;
  signOut: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = use(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
