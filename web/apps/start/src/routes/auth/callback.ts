import { createFileRoute } from '@tanstack/react-router'
import * as oidc from 'openid-client'

import { getOidcConfig } from '@/server/oidc/client'
import {
  loginTxClearCookie,
  readLoginTx,
  sessionSetCookie,
  tokensFromResponse,
} from '@/server/oidc/session'

/**
 * OAuth callback: Keycloak redirects the browser here with `code` + `state`.
 * We complete the code+PKCE exchange (which also validates `state` against the
 * value stashed at /auth/sign-in), store the token set in the session cookie,
 * clear the transaction cookie, and redirect to the original destination.
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
          const response = await oidc.authorizationCodeGrant(config, new URL(request.url), {
            pkceCodeVerifier: tx.code_verifier,
            expectedState: tx.state,
          })
          tokens = tokensFromResponse(response)
        } catch {
          // Exchange / state / PKCE failure — drop the transaction and restart.
          return new Response(null, {
            status: 302,
            headers: { location: '/auth/sign-in', 'set-cookie': loginTxClearCookie(request) },
          })
        }

        // return_to was sanitized to a same-origin path at /auth/sign-in.
        const headers = new Headers({ location: tx.return_to })
        headers.append('set-cookie', sessionSetCookie(request, tokens))
        headers.append('set-cookie', loginTxClearCookie(request))
        return new Response(null, { status: 302, headers })
      },
    },
  },
})
