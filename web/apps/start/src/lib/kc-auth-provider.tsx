'use client';

import { AuthContext, type AuthUser } from '@pivox/features/auth';
import { useCallback, useMemo } from 'react';

/**
 * Keycloak BFF auth provider for the web app.
 *
 * Supplies the platform-neutral `AuthContext` the shared app shell + gates
 * consume. Unlike the Firebase `AuthProvider` (Electron), there is NO client
 * auth SDK here: the session lives entirely in the httpOnly `__pivox_oidc`
 * cookie, and the SSR gate (`_app` / `create-org` `beforeLoad`) resolves the
 * user before render. So `user` is injected as a prop (already known by the
 * time we paint) and `loading` is always `false` — there is nothing to wait
 * for on the client.
 *
 * `signOut` submits a same-origin form POST to `/auth/logout`, a server handler
 * that clears the cookie and bounces through Keycloak's RP-initiated end-session
 * endpoint. It's a POST (not a GET navigation) because logout deletes the
 * server-side session row — a GET would be CSRF-able by any cross-site link. A
 * real form submit (not `fetch`) is required so the handler's 302 through
 * Keycloak is followed as a top-level navigation; a client-side router
 * navigation wouldn't invoke the server handler at all. The returned promise
 * never resolves — the document unload takes over — which is the intended
 * "halt the caller" behavior.
 */
export function KeycloakAuthProvider({
  user,
  children,
}: {
  user: AuthUser;
  children: React.ReactNode;
}) {
  const signOut = useCallback(async () => {
    // Same-origin form POST (carries Origin + Sec-Fetch-Site=same-origin, which
    // the /auth/logout handler checks). A real submit drives the top-level nav so
    // the handler's 302 through Keycloak end-session is followed by the browser.
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = '/auth/logout';
    document.body.appendChild(form);
    form.submit();
    // The full-page nav is in flight; hold the caller so nothing renders a
    // post-sign-out frame against a still-present user.
    await new Promise<never>(() => {});
  }, []);

  const value = useMemo(
    () => ({ user, loading: false, signOut }),
    [user, signOut],
  );

  return <AuthContext value={value}>{children}</AuthContext>;
}
