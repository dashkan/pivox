import { createFileRoute } from '@tanstack/react-router'

import { isTokenFresh, readSessionId, refreshSession, sessionClearCookie } from '@/server/oidc/session'
import { deleteSession, getSession } from '@/server/oidc/session-store'

/** Backend origin the BFF forwards to (e.g. https://pivox.example). */
const BACKEND = process.env.PIVOX_API_URL

// Client-spoofable / hop-by-hop request headers we never forward upstream. Envoy
// stays the sole authority on x-forwarded-*; the cookie/host/length are wrong for
// the rewritten request; the rest are hop-by-hop.
const STRIP_REQUEST_HEADERS = [
  'cookie',
  'host',
  'content-length',
  'x-forwarded-for',
  'x-forwarded-host',
  'x-forwarded-proto',
  'x-forwarded-port',
  'x-real-ip',
  'forwarded',
  'connection',
  'keep-alive',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'proxy-authorization',
]

// Upstream response headers we must NOT pass straight through: undici already
// decoded the body (so content-encoding/length are stale), and the backend's
// cookies/hop-by-hop headers are not the browser's business via the BFF.
const STRIP_RESPONSE_HEADERS = [
  'content-encoding',
  'content-length',
  'set-cookie',
  'connection',
  'keep-alive',
  'transfer-encoding',
  'upgrade',
]

function json(status: number, body: unknown, extraHeaders?: Record<string, string>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...extraHeaders },
  })
}

/**
 * BFF reverse proxy: /api/v1/* -> <PIVOX_API_URL>/v1/* with the session's access
 * token injected as a Bearer. The token is refreshed proactively within a skew
 * window before forwarding (a backend 401 is still passed through — clients treat
 * 401 as "re-authenticate"). On rotation only the server-side row changes; the
 * cookie holds a stable opaque id, so there is no Set-Cookie on the happy path.
 * A dead/unrefreshable session deletes the row and clears the cookie. Only /v1/*
 * is reachable by construction — /internal/* is never proxied.
 */
export const Route = createFileRoute('/api/v1/$')({
  server: {
    handlers: {
      ANY: async ({ request, params }) => {
        if (!BACKEND) return json(500, { error: 'backend_not_configured' })

        // CSRF: the session is a cookie, so reject cross-site state-changing
        // requests. Same-origin fetch/XHR always sends Origin; Sec-Fetch-Site is
        // a browser-set, cross-site-unforgeable secondary signal. Safe methods
        // are exempt (they must not mutate).
        if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method)) {
          const self = new URL(request.url).origin
          const origin = request.headers.get('origin')
          const sameOrigin = origin === self || request.headers.get('sec-fetch-site') === 'same-origin'
          if (!sameOrigin) return json(403, { error: 'csrf' })
        }

        const sessionId = readSessionId(request)
        if (!sessionId) return json(401, { error: 'unauthenticated' })
        let session = await getSession(sessionId)
        if (!session) return json(401, { error: 'unauthenticated' })

        // Transparent refresh before forwarding (single-flighted in refreshSession,
        // which also persists the rotated set back to the row — the cookie/id is
        // stable, so nothing is written back to the browser on success).
        if (!isTokenFresh(session) && session.refresh_token) {
          try {
            session = await refreshSession(sessionId)
          } catch {
            // Refresh failed → the session is dead. Drop the row so the spent id
            // can't be retried, and clear the cookie.
            await deleteSession(sessionId)
            return json(401, { error: 'session_expired' }, { 'set-cookie': sessionClearCookie(request) })
          }
        }

        const splat = params._splat ?? ''
        // Reject encoded dot segments: new URL() normalizes literal `..` (caught
        // by the pathname guard below) but leaves %2e encoded, which the backend
        // could decode to escape /v1 -> /internal.
        if (/%2e/i.test(splat)) return json(400, { error: 'bad_path' })

        const incoming = new URL(request.url)
        const target = new URL(`/v1/${splat}`, BACKEND)
        target.search = incoming.search
        if (target.origin !== new URL(BACKEND).origin || !target.pathname.startsWith('/v1/')) {
          return json(400, { error: 'bad_path' })
        }

        const headers = new Headers(request.headers)
        for (const name of STRIP_REQUEST_HEADERS) headers.delete(name)
        // Set AFTER copying so a client-supplied Authorization can't override it.
        headers.set('authorization', `Bearer ${session.access_token}`)

        const hasBody = request.method !== 'GET' && request.method !== 'HEAD'
        const init: RequestInit & { duplex?: 'half' } = {
          method: request.method,
          headers,
          redirect: 'manual',
        }
        if (hasBody) {
          init.body = request.body
          init.duplex = 'half' // required by fetch when streaming a request body
        }

        const upstream = await fetch(target, init)

        const responseHeaders = new Headers(upstream.headers)
        for (const name of STRIP_RESPONSE_HEADERS) responseHeaders.delete(name)
        return new Response(upstream.body, {
          status: upstream.status,
          statusText: upstream.statusText,
          headers: responseHeaders,
        })
      },
    },
  },
})
