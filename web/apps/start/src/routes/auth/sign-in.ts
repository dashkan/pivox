import { createFileRoute } from '@tanstack/react-router'
import * as oidc from 'openid-client'

import { callbackUrl, getOidcConfig, OIDC_SCOPE } from '@/server/oidc/client'
import { loginTxSetCookie } from '@/server/oidc/session'

/**
 * Same-origin destination only. Resolves the candidate against our own origin
 * and keeps it only when the origin matches — this rejects absolute URLs as well
 * as the `/\evil.com` backslash trick browsers re-interpret as protocol-relative.
 */
function sanitizeReturnTo(value: string | null, origin: string): string {
  if (!value) return '/'
  try {
    const url = new URL(value, origin)
    return url.origin === origin ? url.pathname + url.search + url.hash : '/'
  } catch {
    return '/'
  }
}

/**
 * Login initiator: starts the Authorization Code + PKCE flow and redirects the
 * browser to Keycloak. The PKCE verifier + state + return-to are stashed in a
 * short-lived httpOnly transaction cookie that /auth/callback consumes.
 */
export const Route = createFileRoute('/auth/sign-in')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const config = await getOidcConfig()
        const codeVerifier = oidc.randomPKCECodeVerifier()
        const codeChallenge = await oidc.calculatePKCECodeChallenge(codeVerifier)
        const state = oidc.randomState()
        const requestUrl = new URL(request.url)
        const returnTo = sanitizeReturnTo(requestUrl.searchParams.get('return'), requestUrl.origin)

        const authUrl = oidc.buildAuthorizationUrl(config, {
          redirect_uri: callbackUrl(request).href,
          scope: OIDC_SCOPE,
          code_challenge: codeChallenge,
          code_challenge_method: 'S256',
          state,
        })

        return new Response(null, {
          status: 302,
          headers: {
            location: authUrl.href,
            'set-cookie': loginTxSetCookie(request, { code_verifier: codeVerifier, state, return_to: returnTo }),
          },
        })
      },
    },
  },
})
