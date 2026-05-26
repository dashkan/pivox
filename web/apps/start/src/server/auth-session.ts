import { createServerFn } from '@tanstack/react-start';
import {
  deleteCookie,
  getCookie,
  setCookie,
} from '@tanstack/react-start/server';
import { getAuth } from 'firebase-admin/auth';

import { firebaseAdmin } from './firebase/admin';

import type { DecodedIdToken } from 'firebase-admin/auth';

/**
 * Server-side Firebase session-cookie management for the start app.
 *
 * Background — why a session cookie at all
 * ────────────────────────────────────────
 * The Firebase JS SDK keeps auth state in IndexedDB on the client. The
 * Pivox start app does SSR (TanStack Start), so on the very first
 * request the SERVER has no idea whether the visitor is signed in —
 * `useAuth()` returns `loading: true, user: null` during SSR and the
 * gate components render a Loading… splash. The client then hydrates,
 * Firebase resolves, the gate re-renders with the real user, and the
 * SPA path proceeds. UX cost: every authenticated route ships a
 * Loading… HTML before transitioning to real content.
 *
 * The fix is to persist a Firebase **session cookie** alongside the
 * Firebase JS SDK's IndexedDB storage. Client mints an ID token after
 * sign-in and POSTs it to `createSession`, which exchanges the
 * short-lived ID token for a long-lived session cookie (max 14 days
 * per Firebase) and Set-Cookie's it on the response. Every subsequent
 * request sends the cookie; the server reads + verifies it via
 * `getServerSession` and knows the user identity before the first byte
 * of HTML is sent.
 *
 * Lifecycle of a cookie expiry
 * ────────────────────────────
 * Firebase session cookies expire after the configured `expiresIn`
 * (we use the 14-day maximum). When that happens:
 *   - Server-side gate sees the cookie present but `verifySessionCookie`
 *     throws → server redirects to `/auth/verify-session?return=<url>`.
 *   - The verify-session route is a client-only interstitial that asks
 *     the Firebase JS SDK for a fresh ID token (which IS available —
 *     the JS SDK's refresh token persists indefinitely until revoked)
 *     and re-calls `createSession` to mint a new cookie, then redirects
 *     to the original target.
 *   - If the JS SDK has no user either (genuine sign-out, password
 *     change, account disabled — anything that revokes the refresh
 *     token), the recovery fails and the interstitial falls through
 *     to `/auth/login`.
 *
 * The presence-vs-validity distinction in the gate
 * ────────────────────────────────────────────────
 * `beforeLoad` MUST distinguish "no cookie at all" (cold visit, no
 * prior session on this browser) from "cookie present but invalid"
 * (expired / revoked):
 *   - No cookie → redirect /auth/login directly (no interstitial; the
 *     user has nothing to recover from).
 *   - Cookie present + invalid → redirect /auth/verify-session
 *     (give the client a chance to re-mint silently).
 *
 * `getServerSession` returns both pieces of info in one round-trip
 * (`{ user, cookiePresent }`); the gate branches on `cookiePresent`
 * when `user` is null.
 *
 * checkRevoked trade-off
 * ──────────────────────
 * `verifySessionCookie(cookie, true)` makes Firebase hit its backend
 * to confirm the cookie hasn't been revoked (sign-out elsewhere,
 * password change, admin-disabled account). This is one extra RPC per
 * authenticated request — at every navigation through the `_app`
 * gate, that's perceptible latency on every link click.
 *
 * We DON'T pass checkRevoked: it stays default-false. Revocations
 * still propagate via the proactive refresh path: AuthProvider's
 * `onTokenRefresh` calls `createSession` (which uses `verifyIdToken`,
 * NOT cookie verification) on every Firebase ID-token rotation
 * (~every 55 min while active). If the underlying refresh token was
 * revoked, `getIdToken()` throws on the client, the refresh fails,
 * and the next navigation hits an expired cookie → the verify-session
 * recovery flow fires, also fails (refresh token revoked), and the
 * user lands on /auth/login. Net latency from revocation to forced
 * sign-out: bounded by the token rotation interval (~55 min).
 *
 * Apps that need immediate revocation propagation should re-add
 * `verifySessionCookie(cookie, true)` and accept the per-navigation
 * latency, or move the check to a separate periodic worker.
 */

const COOKIE_NAME = '__pivox_session';

/**
 * 14 days in seconds (Firebase's documented maximum for session
 * cookies). Setting the maxAge identically on the cookie ensures the
 * browser stops sending it the moment the server-side cookie would
 * stop being valid, so the user lands on the verify-session
 * interstitial via the no-cookie branch rather than the
 * cookie-present-but-invalid branch — cleaner UX on the boundary.
 */
const COOKIE_MAX_AGE_SECONDS = 60 * 60 * 24 * 14;

/**
 * Shape returned to the client after a successful session establishment
 * or verification. Mirrors the subset of Firebase `DecodedIdToken` the
 * client actually consumes (route context, AuthProvider hydration).
 * Keep this lean — anything more is just exfiltrating PII through the
 * router state hash on the wire.
 */
export interface ServerSession {
  uid: string;
  email: string | null;
  emailVerified: boolean;
  displayName: string | null;
  photoURL: string | null;
}

/**
 * Combined session-state shape returned by `getServerSession`. Bundles
 * the verified user (or null) with a cookie-presence flag so a single
 * round-trip from `beforeLoad` covers all three branching cases:
 *
 *   - user !== null              → authenticated; render route
 *   - user === null + cookiePresent: true  → expired/revoked; redirect
 *                                            to /auth/verify-session
 *                                            for silent recovery
 *   - user === null + cookiePresent: false → cold visit; redirect to
 *                                            /auth/login directly
 *
 * Splitting cookie-presence into its own helper would force two
 * round-trips on client-side navigation (beforeLoad runs on both
 * server and client) and complicate caching.
 */
export interface ServerSessionStatus {
  user: ServerSession | null;
  cookiePresent: boolean;
}

/**
 * Map a verified Firebase token to our wire shape. The cast strips
 * `DecodedIdToken`'s `[key: string]: any` index signature so the
 * known-field accesses below type-check cleanly — without it, every
 * property collapses to `any` and trips
 * `@typescript-eslint/no-unsafe-assignment`. Only the fields we
 * actually consume are extracted; everything else (auth_time, iat,
 * exp, custom claims, ...) stays inside the decoded token.
 */
function toServerSession(decoded: DecodedIdToken): ServerSession {
  const claims = decoded as {
    uid: string;
    email?: string;
    email_verified?: boolean;
    name?: string;
    picture?: string;
  };
  return {
    uid: claims.uid,
    email: claims.email ?? null,
    emailVerified: Boolean(claims.email_verified),
    displayName: claims.name ?? null,
    photoURL: claims.picture ?? null,
  };
}

/**
 * Mint a session cookie from a client-supplied Firebase ID token.
 *
 * Called by the client right after a successful Firebase sign-in
 * (password, SSO via broker, social) — see useLogin / useRegistration
 * `onSuccess` plumbing. The ID token is verified server-side via
 * Firebase Admin before being exchanged for the cookie, so a forged
 * token can't poison the session.
 */
export const createSession = createServerFn({ method: 'POST' })
  .inputValidator((data: { idToken: string }) => {
    if (!data.idToken) {
      throw new Error('createSession: idToken is required');
    }
    return data;
  })
  .handler(async ({ data }): Promise<ServerSession> => {
    const auth = getAuth(firebaseAdmin());
    // Verify the ID token first so we trust the uid + claims before
    // minting a long-lived cookie. createSessionCookie also validates
    // the token, but verifying explicitly gives us the decoded shape
    // for the return value in a single round-trip.
    const decoded: DecodedIdToken = await auth.verifyIdToken(data.idToken);
    const sessionCookie = await auth.createSessionCookie(data.idToken, {
      expiresIn: COOKIE_MAX_AGE_SECONDS * 1000,
    });
    setCookie(COOKIE_NAME, sessionCookie, {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      path: '/',
      maxAge: COOKIE_MAX_AGE_SECONDS,
    });
    return toServerSession(decoded);
  });

/**
 * Clear the session cookie + revoke the user's refresh tokens.
 *
 * Called by the client during sign-out. Revoking refresh tokens at the
 * same time means "sign out" actually invalidates the session
 * everywhere (other tabs, other devices), which is what users expect
 * from a sign-out button. If we only deleted the cookie, the Firebase
 * JS SDK on another tab could silently mint a new cookie via the
 * still-valid refresh token.
 *
 * Always returns success — a sign-out that fails server-side
 * shouldn't block the client's Firebase signOut + redirect; the worst
 * case is a stale cookie that fails verification on next request.
 */
export const clearSession = createServerFn({ method: 'POST' }).handler(
  async () => {
    const cookie = getCookie(COOKIE_NAME);
    if (cookie) {
      try {
        const auth = getAuth(firebaseAdmin());
        const decoded: DecodedIdToken = await auth.verifySessionCookie(cookie);
        await auth.revokeRefreshTokens(decoded.uid);
      } catch {
        // Cookie is already invalid or revoked — nothing to do
        // beyond deleting it below.
      }
    }
    deleteCookie(COOKIE_NAME, { path: '/' });
  },
);

/**
 * Read + verify the session cookie. Returns the user identity for
 * server-side gates (`beforeLoad`) and AuthProvider hydration.
 *
 * Returns `null` for BOTH "no cookie" and "cookie invalid". The
 * caller inspects `getCookie('__pivox_session')` to distinguish if
 * the routing decision depends on it (cold visit vs expired session).
 *
 * `checkRevoked: true` makes this hit Firebase backend on every call
 * — accepted cost for the security guarantee that a revoked cookie
 * stops working immediately rather than at natural expiry.
 */
export const getServerSession = createServerFn({ method: 'GET' }).handler(
  async (): Promise<ServerSessionStatus> => {
    const cookie = getCookie(COOKIE_NAME);
    if (!cookie) return { user: null, cookiePresent: false };
    try {
      const auth = getAuth(firebaseAdmin());
      // checkRevoked stays default-false — see the trade-off note in
      // the file header. Revocations propagate via proactive refresh
      // within ~55 min instead of synchronously, with the win that
      // every authenticated navigation skips a Firebase backend RPC.
      const decoded: DecodedIdToken = await auth.verifySessionCookie(cookie);
      return { user: toServerSession(decoded), cookiePresent: true };
    } catch {
      return { user: null, cookiePresent: true };
    }
  },
);
