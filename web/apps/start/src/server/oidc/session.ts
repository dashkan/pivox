import { parseCookie, stringifySetCookie } from 'cookie'
import * as oidc from 'openid-client'

import { getOidcConfig } from './client'

import type { TokenEndpointResponse } from 'openid-client'

/**
 * BFF session + login-transaction cookies for the OIDC (Keycloak) flow.
 *
 * Two httpOnly cookies:
 *  - SESSION_COOKIE holds the token set (access + refresh + id + expiry). The
 *    proxy reads the access token to inject the Bearer; /auth/refresh and
 *    /auth/logout read the refresh/id tokens.
 *  - TX_COOKIE holds the short-lived login transaction (PKCE verifier + state +
 *    return-to) between /auth/sign-in and /auth/callback.
 *
 * Both are httpOnly + SameSite=Lax + Path=/, and Secure whenever the request
 * arrived over HTTPS (determined per-request, not from NODE_ENV, so a prod-like
 * deployment can't accidentally ship cookies without Secure). Cookies are read
 * from the request's Cookie header and written as Set-Cookie on the Response the
 * handler returns — explicit, not via ambient request context.
 *
 * Size note: the token set lives in a single cookie. Keycloak access+refresh+id
 * tokens are typically under the ~4KB per-cookie limit; if a deployment inflates
 * them with many roles/claims and overflows, the browser silently drops the
 * cookie — split the access token into its own cookie at that point.
 */

const SESSION_COOKIE = '__pivox_oidc'
const TX_COOKIE = '__pivox_oidc_tx'

// Bounded by the Keycloak refresh-token lifetime; refreshed on every successful
// token refresh, so this is just the idle ceiling.
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

// --- session token set ---

export function readSession(request: Request): SessionTokens | undefined {
  return readJsonCookie(request, SESSION_COOKIE) as SessionTokens | undefined
}

export function sessionSetCookie(request: Request, tokens: SessionTokens): string {
  return buildSetCookie(SESSION_COOKIE, JSON.stringify(tokens), SESSION_MAX_AGE_SECONDS, isSecure(request))
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

// --- token refresh (single-flighted) ---

const inflightRefresh = new Map<string, Promise<SessionTokens>>()

/**
 * Refreshes the session for a refresh token, single-flighting concurrent callers.
 * A browser fires many API calls at once; without this each in-flight proxy
 * request would spend the SAME refresh token, and with Keycloak refresh-token
 * rotation the later ones trip reuse-detection and revoke the whole token family,
 * forcing a mid-session logout. Per process only — multi-replica deployments
 * still race across instances, so keep IdP refresh-token reuse tolerant or accept
 * the skew there.
 */
export async function refreshSession(refreshToken: string): Promise<SessionTokens> {
  const existing = inflightRefresh.get(refreshToken)
  if (existing) return existing

  const promise = (async () => {
    const config = await getOidcConfig()
    const tokens = tokensFromResponse(await oidc.refreshTokenGrant(config, refreshToken))
    // Keycloak rotates refresh tokens; if a deployment doesn't, keep the old one.
    if (!tokens.refresh_token) tokens.refresh_token = refreshToken
    return tokens
  })()

  inflightRefresh.set(refreshToken, promise)
  try {
    return await promise
  } finally {
    inflightRefresh.delete(refreshToken)
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
