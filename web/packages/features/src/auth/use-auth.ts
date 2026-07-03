'use client';

import { createContext, use } from 'react';

/**
 * Platform-neutral authenticated user for the app shell + route gates.
 *
 * `id` is the Pivox identity id — the Keycloak `sub` (which equals
 * `identities.id`) on both platforms: the web BFF resolves it from the
 * server-side session, and the Electron provider decodes it from the id_token
 * over IPC. `null` id means the identity hasn't been provisioned yet.
 *
 * Deliberately transport-agnostic: each app supplies its own provider — the web
 * app's BFF-backed KeycloakAuthProvider, or Electron's IPC-backed one — that
 * maps its Keycloak session into this shape.
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
