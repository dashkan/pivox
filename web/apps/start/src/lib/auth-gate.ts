import { redirect } from '@tanstack/react-router';

import {
  getServerSession,
  type ServerSession,
  type ServerSessionStatus,
} from '@/server/oidc-session';

/**
 * A resolved Keycloak BFF session, guaranteed to carry a user. Returned by
 * {@link requireKcSession} once the gate has passed.
 */
export interface AuthedSession extends ServerSessionStatus {
  user: ServerSession;
}

/**
 * SSR/CSR auth gate for authenticated routes. Reads the BFF session and, when
 * there's no user, sends the browser to the `/auth/sign-in` SERVER handler.
 *
 * `/auth/sign-in` is NOT a client route — it's a server handler that starts the
 * OAuth Authorization Code + PKCE flow. It must be reached via a full-document
 * navigation:
 *   - SSR pass (`typeof window === 'undefined'`): throw a `redirect` with an
 *     absolute-path `href`, which TanStack Start turns into an HTTP 302 whose
 *     `Location` the browser follows with a real GET — invoking the handler.
 *   - Client pass: a router redirect would do an SPA navigation that never hits
 *     the handler, so use `window.location` and then halt (the document unload
 *     takes over). The trailing throw is unreachable at runtime but gives the
 *     branch a terminating control-flow edge so `user` narrows to non-null
 *     below.
 *
 * The original return path is preserved via `?return=<path>`, which the sign-in
 * handler stashes and `/auth/callback` redirects back to after login.
 */
export async function requireKcSession(location: {
  pathname: string;
  searchStr: string;
}): Promise<AuthedSession> {
  const status = await getServerSession();
  if (!status.user) {
    const target = `/auth/sign-in?return=${encodeURIComponent(
      location.pathname + location.searchStr,
    )}`;
    if (typeof window !== 'undefined') {
      window.location.href = target;
      await new Promise<never>(() => {});
    }
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ href: target, reloadDocument: true });
  }
  return { ...status, user: status.user };
}
