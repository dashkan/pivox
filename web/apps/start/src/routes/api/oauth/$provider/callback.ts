import { createFileRoute } from '@tanstack/react-router'
import { resolveProvider } from '#/server/oauth/resolver'
import { verifyState } from '#/server/oauth/state'
import type { TokenExchangeResponse } from '#/server/oauth/providers'

/**
 * Exit point: `GET /api/oauth/:provider/callback?code=…&state=…`
 *
 * Provider redirects here after the user approves. We:
 *   1. Verify the HMAC-signed `state` (binds provider + returnUrl
 *      + issued-at to the original authorize request).
 *   2. Exchange the authorization code for the provider's access /
 *      id token (server-to-server, carries the client_secret).
 *   3. Redirect to the caller's `return_url` with the normalized
 *      credential in the URL fragment.
 *
 * Why URL fragment (`#`) instead of query (`?`):
 *   - Fragments are never sent to servers in HTTP requests, so the
 *     token can't accidentally leak through referrer headers or
 *     server access logs on the `return_url` target.
 *   - Native's `ASWebAuthenticationSession` still receives the full
 *     URL including fragment and passes it to the app for parsing.
 *
 * What's returned in the fragment:
 *   - `provider` — which provider authorized (for native to know
 *     which Firebase credential adapter to use).
 *   - `kind` — `github_access_token` | `oidc_id_token`.
 *   - `token` — the raw provider token matching `kind`.
 *   Native inspects `kind` and constructs the appropriate Firebase
 *   credential (GithubAuthProvider.credential vs
 *   OAuthProvider.credential). This keeps the route-level contract
 *   uniform while letting the client handle the small amount of
 *   per-provider credential wiring.
 *
 * Later (provider-agnostic upgrade): mint a Firebase custom token
 * here server-side via Admin SDK and return THAT instead. Native
 * would only know `signInWithCustomToken`. See
 * `server/firebase/admin.ts` — the scaffolding is ready, the user
 * lookup/provision logic is the remaining work.
 */
export const Route = createFileRoute('/api/oauth/$provider/callback')({
  server: {
    handlers: {
      GET: async ({ request, params }) => {
        const provider = params.provider
        const url = new URL(request.url)
        const code = url.searchParams.get('code')
        const stateToken = url.searchParams.get('state')
        const providerError = url.searchParams.get('error')

        if (providerError) {
          // User denied, or provider returned an error. Bubble it to
          // `return_url` if we can still recover it from state.
          return respondWithProviderError(stateToken, providerError, url.searchParams)
        }

        if (!code || !stateToken) {
          return json({ error: 'missing_code_or_state' }, 400)
        }

        const state = verifyState(stateToken)
        if (!state) {
          return json({ error: 'invalid_state' }, 400)
        }
        if (state.p !== provider) {
          return json({ error: 'provider_mismatch' }, 400)
        }

        let cfg
        try {
          cfg = await resolveProvider(provider)
        } catch (err) {
          return json({ error: 'provider_misconfigured', detail: String(err) }, 500)
        }
        if (!cfg) {
          return json({ error: 'unknown_provider' }, 404)
        }

        const brokerBase = process.env.OAUTH_BROKER_URL
        if (!brokerBase) {
          return json({ error: 'broker_url_missing' }, 500)
        }

        // Exchange the code for the provider's token. Every provider
        // speaks the same OAuth 2.0 authorization-code grant here, so
        // the request shape is uniform — only `tokenRequestHeaders`
        // differs (GitHub defaults to form-encoded response unless we
        // ask for JSON).
        const body = new URLSearchParams({
          grant_type: 'authorization_code',
          code,
          client_id: cfg.clientId,
          client_secret: cfg.clientSecret,
          redirect_uri: `${brokerBase}/api/oauth/${provider}/callback`,
        })

        let tokenResp: TokenExchangeResponse
        try {
          const res = await fetch(cfg.tokenUrl, {
            method: 'POST',
            headers: {
              'content-type': 'application/x-www-form-urlencoded',
              ...(cfg.tokenRequestHeaders ?? {}),
            },
            body: body.toString(),
          })
          if (!res.ok) {
            return json({ error: 'token_exchange_failed', status: res.status }, 502)
          }
          tokenResp = (await res.json()) as TokenExchangeResponse
        } catch (err) {
          return json({ error: 'token_exchange_error', detail: String(err) }, 502)
        }

        let credential
        try {
          credential = cfg.toFirebaseCredential(tokenResp)
        } catch (err) {
          return json({ error: 'credential_adapt_failed', detail: String(err) }, 502)
        }

        // Build the return URL with the credential in the fragment.
        const returnUrl = new URL(state.r)
        const fragment = new URLSearchParams({
          provider,
          kind: credential.kind,
          token:
            credential.kind === 'github_access_token'
              ? credential.accessToken
              : credential.idToken,
        })
        if (credential.kind === 'oidc_id_token' && credential.accessToken) {
          fragment.set('access_token', credential.accessToken)
        }
        returnUrl.hash = fragment.toString()

        return Response.redirect(returnUrl.toString(), 302)
      },
    },
  },
})

// If the provider itself reported an error (user denied, scope
// rejected, etc.), try to redirect back to the caller's return_url
// with the error payload in the fragment so the native/web caller
// can surface it. Falls back to a JSON error if state is unusable.
function respondWithProviderError(
  stateToken: string | null,
  errorCode: string,
  params: URLSearchParams
): Response {
  if (!stateToken) {
    return json({ error: 'provider_error', code: errorCode }, 400)
  }
  const state = verifyState(stateToken)
  if (!state) {
    return json({ error: 'provider_error', code: errorCode }, 400)
  }
  try {
    const returnUrl = new URL(state.r)
    const frag = new URLSearchParams({
      error: errorCode,
      error_description: params.get('error_description') ?? '',
    })
    returnUrl.hash = frag.toString()
    return Response.redirect(returnUrl.toString(), 302)
  } catch {
    return json({ error: 'provider_error', code: errorCode }, 400)
  }
}

function json(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
