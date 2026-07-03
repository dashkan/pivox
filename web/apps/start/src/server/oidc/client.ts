import { createConfigProvider, type ConfigProvider } from '@pivox/oidc'

import type * as oidc from 'openid-client'

/**
 * OIDC (Keycloak) client configuration for the `start` BFF.
 *
 * The BFF runs the OAuth Authorization Code + PKCE flow server-side using the
 * confidential `start` client. The shared {@link createConfigProvider} handles
 * lazy, per-process memoized discovery (with retry-on-failure) — server boot
 * isn't coupled to Keycloak readiness; the first /auth/login pays the discovery
 * cost, by which point the IdP is reachable.
 */

const ISSUER = 'PIVOX_OIDC_ISSUER'
const CLIENT_ID = 'PIVOX_OIDC_CLIENT_ID'
const CLIENT_SECRET = 'PIVOX_OIDC_CLIENT_SECRET'

function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} is required for the OIDC BFF`)
  }
  return value
}

let provider: ConfigProvider | undefined

/**
 * Discovers and returns the memoized Keycloak {@link oidc.Configuration} for the
 * confidential `start` client. Reading the env is deferred to first call (not
 * module load) so a missing var surfaces at login, not at import.
 */
export function getOidcConfig(): Promise<oidc.Configuration> {
  if (!provider) {
    provider = createConfigProvider({
      issuer: required(ISSUER),
      clientId: required(CLIENT_ID),
      clientSecret: required(CLIENT_SECRET),
    })
  }
  return provider()
}

/**
 * Scopes requested at login. `openid` is required for an ID token; `profile` and
 * `email` feed the backend's lazy provisioning from standard claims.
 */
export const OIDC_SCOPE = 'openid profile email'

/**
 * Allowed public origins (PIVOX_PUBLIC_ORIGINS, comma-separated). When set, the
 * BFF only builds redirect/logout URLs for these origins — defense-in-depth over
 * Keycloak's redirect-URI registration, so a spoofed Host (or a future wildcard
 * KC misconfig) can't turn into a redirect to an attacker origin. Unset = allow
 * the request's own origin (dev convenience).
 */
function allowedOrigins(): string[] | null {
  const raw = process.env.PIVOX_PUBLIC_ORIGINS
  if (!raw) return null
  return raw
    .split(',')
    .map((o) => o.trim())
    .filter(Boolean)
}

function firstHeaderValue(request: Request, name: string): string | undefined {
  const value = request.headers.get(name)
  return value ? value.split(',')[0].trim() : undefined
}

/**
 * The public origin the browser actually used. envoy/ngrok terminate TLS and
 * forward to `start` over http, so the request's own protocol is wrong — the real
 * scheme comes from x-forwarded-proto, and the public host from x-forwarded-host
 * (or the preserved Host, since envoy doesn't rewrite it). Falls back to the
 * request URL for direct access. Validated against PIVOX_PUBLIC_ORIGINS so we
 * never build a redirect to an unexpected origin.
 *
 * This is THE origin that must match the `start` client's registered redirect
 * URIs — openid-client derives the token-exchange redirect_uri from the callback
 * URL, so sign-in and callback must agree on it exactly.
 */
export function publicOrigin(request: Request): string {
  const url = new URL(request.url)
  const proto = firstHeaderValue(request, 'x-forwarded-proto') ?? url.protocol.replace(/:$/, '')
  const host = firstHeaderValue(request, 'x-forwarded-host') ?? url.host
  const origin = `${proto}://${host}`

  const allow = allowedOrigins()
  if (allow && !allow.includes(origin)) {
    throw new Error(`oidc: request origin ${origin} is not in PIVOX_PUBLIC_ORIGINS`)
  }
  return origin
}

/**
 * Builds the redirect_uri from the validated public origin. Every origin it can
 * produce MUST be a registered Valid Redirect URI on the `start` client.
 */
export function callbackUrl(request: Request): URL {
  return new URL('/auth/callback', publicOrigin(request))
}
