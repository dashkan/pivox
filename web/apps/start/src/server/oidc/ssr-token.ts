import { getRequest } from '@tanstack/react-start/server'

import { isTokenFresh, readSessionId, refreshSession } from './session'
import { getSession } from './session-store'

/**
 * Resolve a valid Keycloak access token for SSR data prefetch.
 *
 * The browser holds no bearer under the BFF — only the opaque session id in the
 * httpOnly `__pivox_oidc` cookie. SSR route loaders that call the Pivox API on
 * the user's behalf read it here: resolve the cookie id to the stored token set,
 * and if the access token is within the refresh skew of expiry, rotate it via
 * the (single-flighted) {@link refreshSession} — which also persists the new set
 * back to the session row.
 *
 * Unlike the cookie-blob design this replaced, there is NO Set-Cookie to write:
 * the cookie holds the stable id, and the rotated tokens live only in the row,
 * which refreshSession updates atomically within its single flight. That removes
 * the spent-refresh-token replay hazard structurally rather than by remembering
 * to overwrite a cookie.
 *
 * Returns `null` when there's no session or the refresh fails. Callers degrade
 * to client-side fetching (the proxy re-attempts auth there).
 */
export async function getSsrAccessToken(): Promise<string | null> {
  const request = getRequest()
  const id = readSessionId(request)
  if (!id) return null
  const session = await getSession(id)
  if (!session?.access_token) return null

  if (!isTokenFresh(session) && session.refresh_token) {
    try {
      // `_app` beforeLoad fires the org + spaces prefetch concurrently, so two
      // getSsrAccessToken calls can hit this branch at once. refreshSession is
      // single-flighted on the session id, so both await the SAME rotation and
      // the SAME single updateSession write — no double-spend of the refresh
      // token, no racing writes.
      const refreshed = await refreshSession(id)
      return refreshed.access_token
    } catch {
      return null
    }
  }
  return session.access_token
}
