'use client';

import { useAuth } from './use-auth';

/**
 * The Pivox identity id of the current user (== `identities.id`). Under Keycloak
 * it's the `sub`; the provider (web BFF or Electron IPC) resolves it into
 * `AuthUser.id`, so this is just a convenience reader over `useAuth().user.id`.
 *
 * Returns:
 *   - `undefined` while auth is still resolving
 *   - `string` once the id is known
 *   - `null` if the user has no Pivox identity yet (unprovisioned) or is signed
 *     out — consumers gate on auth and/or a "complete your account" path
 *
 * The optional argument is deprecated and ignored: the SSR seed is now carried
 * by the provider's `user` (the web app injects it from the `_app` route
 * context), so there's no separate value to thread through.
 */
export function useUserId(
  _deprecatedInitialValue?: string | null,
): string | null | undefined {
  const { user, loading } = useAuth();
  if (loading) return undefined;
  return user ? user.id : null;
}
