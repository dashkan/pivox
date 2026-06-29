import { ACTIVE_ORG } from '@pivox/storage'
import { createFileRoute } from '@tanstack/react-router'
import { stringifySetCookie } from 'cookie'
import * as oidc from 'openid-client'

import { getOidcConfig, publicOrigin } from '@/server/oidc/client'
import { readSessionId, sessionClearCookie } from '@/server/oidc/session'
import { deleteSession, getSession } from '@/server/oidc/session-store'

/**
 * Logout: deletes the server-side session row (revocation), clears the local id
 * cookie, and redirects to Keycloak's RP-initiated end-session endpoint (so the
 * IdP session is terminated too), which returns the browser to the app root. If
 * discovery/end-session is unavailable we still delete the row + clear the cookie
 * and land on the app root.
 *
 * POST-only + same-origin checked: logout is state-changing (it deletes the
 * session row), so exposing it on GET would let any cross-site navigation
 * (a link, window.open, meta-refresh) force-logout the user — CSRF. The client
 * submits a same-origin form POST; this handler rejects anything not same-origin,
 * mirroring the /api/v1 proxy's CSRF guard. The POST still 302s through Keycloak
 * end-session, which the browser follows as a normal top-level navigation.
 */
export const Route = createFileRoute('/auth/logout')({
  server: {
    handlers: {
      POST: async ({ request }) => {
        // CSRF: logout deletes the session row, so reject cross-site requests. A
        // same-origin form POST sends Origin; Sec-Fetch-Site is a browser-set,
        // cross-site-unforgeable secondary signal (and covers the proxy case where
        // Origin != the internal request URL). SameSite=Lax already withholds the
        // session cookie on cross-site POST, so this is defense-in-depth.
        const self = new URL(request.url).origin
        const origin = request.headers.get('origin')
        const sameOrigin = origin === self || request.headers.get('sec-fetch-site') === 'same-origin'
        if (!sameOrigin) return new Response('forbidden', { status: 403 })

        // Resolve the row for the id_token end-session hint, THEN delete it so
        // the session is revoked even if end-session URL building throws below.
        const sessionId = readSessionId(request)
        const session = sessionId ? await getSession(sessionId) : undefined
        if (sessionId) await deleteSession(sessionId)

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

        const headers = new Headers({ location })
        headers.append('set-cookie', sessionClearCookie(request))
        // Also drop user-scoped cookies on logout. Without this, the next user
        // on a shared browser keeps the previous user's ACTIVE_ORG: the `_app`
        // SSR prefetch would then fetch that org's spaces with the NEW user's
        // token — leaking the prior user's org slug + a wrong-org first paint
        // (and a 403 for non-members). The former Firebase clearSession cleared
        // this too; preserve that. Add any future user-scoped cookies here.
        headers.append(
          'set-cookie',
          stringifySetCookie({
            name: ACTIVE_ORG.name,
            value: '',
            path: ACTIVE_ORG.path,
            maxAge: 0,
          }),
        )
        return new Response(null, { status: 302, headers })
      },
    },
  },
})
