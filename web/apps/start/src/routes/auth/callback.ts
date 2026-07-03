import { exchangeAuthorizationCode } from '@pivox/oidc'
import { createFileRoute } from '@tanstack/react-router'

import { getOidcConfig, publicOrigin } from '@/server/oidc/client'
import {
  loginTxClearCookie,
  readLoginTx,
  sessionSetCookie,
} from '@/server/oidc/session'
import { createSession } from '@/server/oidc/session-store'
import { decodeIdTokenClaims } from '@/server/oidc-session'

/**
 * OAuth callback: Keycloak redirects the browser here with `code` + `state`.
 * We complete the code+PKCE exchange (which also validates `state` against the
 * value stashed at /auth/sign-in), persist the token set server-side keyed on a
 * fresh opaque session id, set that id as the session cookie, clear the
 * transaction cookie, and redirect to the original destination.
 */
export const Route = createFileRoute('/auth/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const tx = readLoginTx(request)
        if (!tx) {
          // No/expired/corrupt login transaction — clear it and restart.
          return new Response(null, {
            status: 302,
            headers: { location: '/auth/sign-in', 'set-cookie': loginTxClearCookie(request) },
          })
        }

        const config = await getOidcConfig()
        let tokens
        try {
          // Rebuild the callback URL on the public origin: openid-client derives
          // the token-exchange redirect_uri from this URL, and it must match the
          // redirect_uri sign-in sent (the public origin, not start's internal http).
          const reqUrl = new URL(request.url)
          const currentUrl = new URL(reqUrl.pathname + reqUrl.search, publicOrigin(request))
          tokens = await exchangeAuthorizationCode(config, {
            currentUrl,
            codeVerifier: tx.code_verifier,
            expectedState: tx.state,
          })
        } catch {
          // Exchange / state / PKCE failure — drop the transaction and restart.
          return new Response(null, {
            status: 302,
            headers: { location: '/auth/sign-in', 'set-cookie': loginTxClearCookie(request) },
          })
        }

        // The id_token's `sub` is the Keycloak subject (== Pivox identity id) we
        // index the session row by. A token set with no sub is unusable — treat
        // it as a failed login and restart rather than persisting an orphan row.
        const sub = (tokens.id_token ? decodeIdTokenClaims(tokens.id_token) : undefined)?.sub
        if (!sub) {
          return new Response(null, {
            status: 302,
            headers: { location: '/auth/sign-in', 'set-cookie': loginTxClearCookie(request) },
          })
        }

        const sessionId = await createSession(tokens, sub)

        // return_to was sanitized to a same-origin path at /auth/sign-in.
        const headers = new Headers({ location: tx.return_to })
        headers.append('set-cookie', sessionSetCookie(request, sessionId))
        headers.append('set-cookie', loginTxClearCookie(request))
        return new Response(null, { status: 302, headers })
      },
    },
  },
})
