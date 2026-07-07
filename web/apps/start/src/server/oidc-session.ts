import { type OidcClaims } from '@pivox/oidc';
import { createServerFn } from '@tanstack/react-start';

import { readServerSession } from './oidc-session.server';

// Re-exported so callback.ts keeps a single import site even though the decoder
// now lives in @pivox/oidc (shared with the Electron main process).
export { decodeIdTokenClaims, type OidcClaims } from '@pivox/oidc';

/**
 * SSR session read for the Keycloak BFF. The `_app` (and `/auth/create-org`)
 * gate's `beforeLoad` reads this — via `requireKcSession` — to learn the user
 * identity before the first byte of HTML, so authenticated routes render
 * without a client-side auth flash.
 *
 * The identity comes from decoding the `id_token` in the token set stored
 * server-side under the opaque id carried in our own httpOnly `__pivox_oidc`
 * cookie (persisted by /auth/callback only after openid-client verified the code
 * exchange with Keycloak over TLS). We therefore DECODE-for-display without
 * re-verifying the signature: the row is first-party and the id is unreadable to
 * the client, and the real API authorization uses the access token, which the
 * cloud backend verifies independently. We also ignore the id_token's own `exp`
 * — liveness is governed by the (long-lived) refresh token and enforced at API
 * call time (refresh-or-401), so the gate stays "logged in" across the short
 * access-token lifetime rather than bouncing every few minutes.
 */

/**
 * Wire shape handed to the client (router context / provider hydration). Under
 * Keycloak the user's `id` IS the `sub` — and `sub` == `identities.id` — so there
 * is a single id. `id` here means "the Pivox identity", which is what the backend
 * authorizes against.
 */
export interface ServerSession {
  id: string;
  email: string | null;
  displayName: string | null;
  photoURL: string | null;
}

export interface ServerSessionStatus {
  user: ServerSession | null;
  cookiePresent: boolean;
  /**
   * Keycloak account-console URL (`{issuer}/account`), or `null` when
   * `PIVOX_OIDC_ISSUER` is unset. Carried here so the gate can hand it to the
   * nav-user "Manage Account" action without a separate round-trip — the issuer
   * is server-only config, so the browser can't derive it itself.
   */
  accountConsoleUrl: string | null;
}

/** Map decoded Keycloak claims to the client-facing session shape. */
export function toServerSession(claims: OidcClaims): ServerSession {
  return {
    id: claims.sub ?? '',
    email: claims.email ?? null,
    displayName: claims.name ?? claims.preferred_username ?? null,
    photoURL: claims.picture ?? null,
  };
}

/**
 * `getServerSession` — the RPC bridge the SSR/CSR auth gate calls to read the
 * BFF session before render. Its handler (`readServerSession`) lives in the
 * sibling `oidc-session.server.ts` so the server-only imports it needs (getCookie
 * + the Postgres session store) never reach the client bundle; the client sees
 * only this stub. It returns `{ user, cookiePresent }` in one round-trip so
 * `beforeLoad` can branch — a present-but-unusable cookie still reports
 * `cookiePresent: true` (the caller redirects to sign-in either way).
 */
export const getServerSession = createServerFn({ method: 'GET' }).handler(
  readServerSession,
);
