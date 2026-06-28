import { createFileRoute } from '@tanstack/react-router'
import * as oidc from 'openid-client'

import { getOidcConfig, publicOrigin } from '@/server/oidc/client'
import { readSession, sessionClearCookie } from '@/server/oidc/session'

/**
 * Logout: clears the local session cookie and redirects to Keycloak's
 * RP-initiated end-session endpoint (so the IdP session is terminated too),
 * which returns the browser to the app root. If discovery/end-session is
 * unavailable we still clear the local session and land on the app root.
 */
export const Route = createFileRoute('/auth/logout')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const session = readSession(request)
        let location = '/' // safe relative fallback if origin/end-session is unavailable

        try {
          const origin = publicOrigin(request)
          location = `${origin}/`
          const config = await getOidcConfig()
          const endSession = oidc.buildEndSessionUrl(config, {
            post_logout_redirect_uri: `${origin}/`,
            ...(session?.id_token ? { id_token_hint: session.id_token } : {}),
          })
          location = endSession.href
        } catch {
          // Origin not allowlisted, or discovery/end-session unavailable —
          // local-only logout to the app root.
        }

        return new Response(null, {
          status: 302,
          headers: { location, 'set-cookie': sessionClearCookie(request) },
        })
      },
    },
  },
})
