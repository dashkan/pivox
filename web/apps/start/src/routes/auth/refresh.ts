import { createFileRoute } from '@tanstack/react-router'

import {
  readSession,
  refreshSession,
  sessionClearCookie,
  sessionSetCookie,
} from '@/server/oidc/session'

const JSON_HEADERS = { 'content-type': 'application/json' }

/**
 * Explicit token refresh (POST). The proxy refreshes transparently before
 * forwarding, so this is for clients that want to refresh proactively (e.g. on
 * window focus). On success the session cookie is rewritten with the new tokens;
 * on failure it's cleared and the client should re-authenticate. Refresh is
 * single-flighted in refreshSession, so concurrent callers share one result.
 */
export const Route = createFileRoute('/auth/refresh')({
  server: {
    handlers: {
      POST: async ({ request }) => {
        const session = readSession(request)
        if (!session?.refresh_token) {
          return new Response(JSON.stringify({ error: 'no_session' }), {
            status: 401,
            headers: JSON_HEADERS,
          })
        }

        try {
          const tokens = await refreshSession(session.refresh_token)
          return new Response(JSON.stringify({ ok: true }), {
            status: 200,
            headers: { ...JSON_HEADERS, 'set-cookie': sessionSetCookie(request, tokens) },
          })
        } catch {
          return new Response(JSON.stringify({ error: 'refresh_failed' }), {
            status: 401,
            headers: { ...JSON_HEADERS, 'set-cookie': sessionClearCookie(request) },
          })
        }
      },
    },
  },
})
