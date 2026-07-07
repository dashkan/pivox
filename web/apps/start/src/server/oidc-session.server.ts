import { decodeIdTokenClaims } from '@pivox/oidc';
import { getCookie } from '@tanstack/react-start/server';

import { toServerSession, type ServerSessionStatus } from './oidc-session';
import { SESSION_COOKIE } from './oidc/session';
import { getSession } from './oidc/session-store';

// SERVER-ONLY module (`.server.ts`). The BFF session read touches `getCookie`
// (@tanstack/react-start/server) and the Postgres-backed session store, neither
// of which can exist in the browser. Keeping this handler here — rather than as
// an exported function in the client-reachable oidc-session.ts — is what stops
// TanStack Start from dragging those imports into the client bundle. The client
// gets only the `getServerSession` RPC stub; this file never ships to it.
//
// (Regression history: this used to be an INLINE handler on `createServerFn()`,
// which the compiler strips from the client. Extracting it into an exported
// named function in oidc-session.ts — for unit testing — made it un-strippable,
// so its server-only imports leaked to the browser and crashed on
// `Buffer is not defined` (from `postgres`). Moving it into a `.server.ts`
// keeps it independently testable AND client-safe.)

/**
 * Derive the Keycloak account-console URL from the configured issuer. The
 * account console lives at `{issuer}/account` (e.g.
 * `https://pivox.ngrok.app/realms/acme/account`), which the browser reaches via
 * the same envoy `/realms/` proxy used for SSO login.
 */
function accountConsoleUrl(): string | null {
  const issuer = process.env.PIVOX_OIDC_ISSUER;
  if (!issuer) return null;
  return `${issuer.replace(/\/+$/, '')}/account`;
}

/**
 * Handler body for `getServerSession`, kept as a plain
 * `() => Promise<ServerSessionStatus>` so it can be unit tested without standing
 * up the `createServerFn` runtime. `oidc-session.ts` wires it to `.handler(...)`.
 *
 * Reads the BFF session for SSR gates and returns `{ user, cookiePresent }` in
 * one round-trip so `beforeLoad` can branch: a present-but-unusable cookie still
 * reports `cookiePresent: true` (the caller redirects to sign-in either way).
 */
export async function readServerSession(): Promise<ServerSessionStatus> {
  const accountUrl = accountConsoleUrl();
  const id = getCookie(SESSION_COOKIE);
  if (!id)
    return { user: null, cookiePresent: false, accountConsoleUrl: accountUrl };
  // The cookie carries an opaque id; resolve it to the token set (lazy-expiry
  // applied) before we can read any identity from it.
  const tokens = await getSession(id);
  if (!tokens)
    return { user: null, cookiePresent: true, accountConsoleUrl: accountUrl };
  const idToken = tokens.id_token;
  if (!idToken)
    return { user: null, cookiePresent: true, accountConsoleUrl: accountUrl };
  const claims = decodeIdTokenClaims(idToken);
  if (!claims?.sub)
    return { user: null, cookiePresent: true, accountConsoleUrl: accountUrl };
  return {
    user: toServerSession(claims),
    cookiePresent: true,
    accountConsoleUrl: accountUrl,
  };
}
