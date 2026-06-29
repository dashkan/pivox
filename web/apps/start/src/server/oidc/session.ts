import { parseCookie, stringifySetCookie } from 'cookie'
import * as oidc from 'openid-client'

import { getOidcConfig } from './client'
import { updateSession, getSession } from './session-store'

import type { TokenEndpointResponse } from 'openid-client'

/**
 * BFF session + login-transaction cookies for the OIDC (Keycloak) flow.
 *
 * Two httpOnly cookies:
 *  - SESSION_COOKIE holds an OPAQUE session id (32 random bytes, base64url). The
 *    token set lives server-side in the `web_sessions` table (see
 *    `./session-store`); the id is the only thing the browser carries, and it IS
 *    the session secret. The proxy resolves the id to the token set to inject the
 *    Bearer (refreshing the row when near expiry); /auth/logout resolves it for
 *    the RP-initiated end-session id-token hint, then deletes the row.
 *  - TX_COOKIE holds the short-lived login transaction (PKCE verifier + state +
 *    return-to) between /auth/sign-in and /auth/callback.
 *
 * Both are httpOnly + SameSite=Lax + Path=/, and Secure whenever the request
 * arrived over HTTPS (determined per-request, not from NODE_ENV, so a prod-like
 * deployment can't accidentally ship cookies without Secure). Cookies are read
 * from the request's Cookie header and written as Set-Cookie on the Response the
 * handler returns — explicit, not via ambient request context.
 *
 * Because only the opaque id rides the cookie, the prior per-cookie size limit
 * on the token set no longer applies, and revocation is now a server-side row
 * delete rather than something the browser has to cooperate with.
 */

export const SESSION_COOKIE = '__pivox_oidc'
const TX_COOKIE = '__pivox_oidc_tx'

// Idle ceiling for the id cookie. The server-side row carries its own sliding
// purge horizon (bumped on each active use); this just bounds how long the
// browser will keep presenting the id.
const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30 // 30 days
const TX_MAX_AGE_SECONDS = 60 * 10 // 10 minutes to complete a login

export interface SessionTokens {
  access_token: string
  refresh_token?: string
  id_token?: string
  /** Epoch milliseconds at which the access token expires. */
  expires_at: number
}

export interface LoginTx {
  code_verifier: string
  state: string
  /** Same-origin path to redirect to after a successful login. */
  return_to: string
}

/**
 * True when the request reached us over HTTPS — directly, or via a
 * TLS-terminating proxy (envoy/ngrok set x-forwarded-proto). Drives the cookie
 * Secure attribute so it's on for every real (https) deployment and off only for
 * plaintext local dev (http://localhost), where Secure cookies wouldn't be sent.
 */
function isSecure(request: Request): boolean {
  if (new URL(request.url).protocol === 'https:') return true
  return request.headers.get('x-forwarded-proto') === 'https'
}

function readCookie(request: Request, name: string): string | undefined {
  const header = request.headers.get('cookie')
  if (!header) return undefined
  return parseCookie(header)[name]
}

// Parses a JSON cookie value, returning undefined when absent or malformed.
// Returns unknown; callers assert the expected shape (a deserialization boundary).
function readJsonCookie(request: Request, name: string): unknown {
  const raw = readCookie(request, name)
  if (!raw) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function buildSetCookie(name: string, value: string, maxAge: number, secure: boolean): string {
  return stringifySetCookie({ name, value, httpOnly: true, secure, sameSite: 'lax', path: '/', maxAge })
}

// --- session id cookie ---

/** Reads the opaque session id from the cookie (no DB hit). */
export function readSessionId(request: Request): string | undefined {
  return readCookie(request, SESSION_COOKIE)
}

export function sessionSetCookie(request: Request, id: string): string {
  return buildSetCookie(SESSION_COOKIE, id, SESSION_MAX_AGE_SECONDS, isSecure(request))
}

export function sessionClearCookie(request: Request): string {
  return buildSetCookie(SESSION_COOKIE, '', 0, isSecure(request))
}

/** Maps an openid-client token response to our stored session shape. */
export function tokensFromResponse(response: TokenEndpointResponse): SessionTokens {
  const expiresInSeconds = response.expires_in ?? 300
  return {
    access_token: response.access_token,
    refresh_token: response.refresh_token,
    id_token: response.id_token,
    expires_at: Date.now() + expiresInSeconds * 1000,
  }
}

// --- token refresh (single-flighted per session) ---

/** Refresh the access token when it's within this window of expiry. */
export const EXPIRY_SKEW_MS = 30_000

const inflightRefresh = new Map<string, Promise<SessionTokens>>()

/**
 * Rotates the access/refresh tokens for a session and persists the new set back
 * to its row, single-flighting concurrent callers on the SESSION ID.
 *
 * A browser fires many API calls at once; without single-flight each in-flight
 * request would spend the SAME refresh token, and with Keycloak refresh-token
 * rotation the later ones trip reuse-detection and revoke the whole token family,
 * forcing a mid-session logout. Keying on the session id (rather than the
 * refresh-token value) also guarantees exactly ONE {@link updateSession} write
 * per burst — the persistence is folded into the single flight so concurrent
 * SSR + proxy callers can't double-write or race a stale set over a fresh one.
 *
 * The current set is re-read from the row INSIDE the flight, never taken from the
 * caller: a flight that starts just after a PRIOR flight rotated the tokens (and
 * cleared the in-flight entry) must spend the row's current refresh token, not the
 * now-rotated value the caller captured before that flight. If the re-read row
 * already holds a still-valid access token, it's returned as-is — no second spend.
 * That closes the sequential A-then-B reuse window structurally, not just the
 * concurrent one. Throws when the row is gone or has no refresh token, which the
 * callers treat as a dead session.
 *
 * Per process only — multi-replica deployments still race across instances, so
 * keep IdP refresh-token reuse tolerant or accept the skew there.
 */
export async function refreshSession(id: string): Promise<SessionTokens> {
  const existing = inflightRefresh.get(id)
  if (existing) return existing

  const promise = (async () => {
    const current = await getSession(id)
    if (!current) throw new Error('refreshSession: no live session row')
    // A concurrent flight may have just rotated the set; reuse it rather than
    // spending the (now stale) refresh token a second time.
    if (current.expires_at - Date.now() >= EXPIRY_SKEW_MS) return current
    if (!current.refresh_token) throw new Error('refreshSession: session has no refresh token')

    const config = await getOidcConfig()
    const tokens = tokensFromResponse(await oidc.refreshTokenGrant(config, current.refresh_token))
    // Keycloak rotates refresh tokens; if a deployment doesn't, keep the old one.
    if (!tokens.refresh_token) tokens.refresh_token = current.refresh_token
    // Persist inside the single flight: the cookie (the id) never changes, so the
    // row is the only place the rotated set lives.
    await updateSession(id, tokens)
    return tokens
  })()

  inflightRefresh.set(id, promise)
  try {
    return await promise
  } finally {
    inflightRefresh.delete(id)
  }
}

// --- login transaction (PKCE + state) ---

export function readLoginTx(request: Request): LoginTx | undefined {
  return readJsonCookie(request, TX_COOKIE) as LoginTx | undefined
}

export function loginTxSetCookie(request: Request, tx: LoginTx): string {
  return buildSetCookie(TX_COOKIE, JSON.stringify(tx), TX_MAX_AGE_SECONDS, isSecure(request))
}

export function loginTxClearCookie(request: Request): string {
  return buildSetCookie(TX_COOKIE, '', 0, isSecure(request))
}
