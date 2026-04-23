import { createFileRoute } from '@tanstack/react-router'
import { resolveProvider } from '#/server/oauth/resolver'
import { signState } from '#/server/oauth/state'

/**
 * Entry point: `GET /api/oauth/:provider/start?return=<url>`
 *
 * Redirects the caller (user's browser or ASWebAuthenticationSession)
 * to the provider's authorize URL with our server-as-redirect-target
 * baked in. The caller's desired return URL travels inside a signed
 * `state` parameter so it round-trips through the provider and comes
 * back to our callback without being tamperable.
 *
 * Caller contract:
 *   - `return` (required): URL the browser should land on after
 *     successful auth. Native passes `pivox://auth-complete`. Web
 *     passes a same-origin path. We allowlist schemes to prevent
 *     open-redirect abuse.
 */
export const Route = createFileRoute('/api/oauth/$provider/start')({
  server: {
    handlers: {
      GET: async ({ request, params }) => {
        const provider = params.provider
        const url = new URL(request.url)
        const returnUrl = url.searchParams.get('return')

        if (!returnUrl) {
          return json({ error: 'missing_return' }, 400)
        }

        if (!isAllowedReturnUrl(returnUrl)) {
          return json({ error: 'disallowed_return_url' }, 400)
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

        const state = signState({ r: returnUrl, p: provider })
        const brokerBase = process.env.OAUTH_BROKER_URL
        if (!brokerBase) {
          return json({ error: 'broker_url_missing' }, 500)
        }

        const authorize = new URL(cfg.authorizeUrl)
        authorize.searchParams.set('client_id', cfg.clientId)
        authorize.searchParams.set(
          'redirect_uri',
          `${brokerBase}/api/oauth/${provider}/callback`
        )
        authorize.searchParams.set('response_type', 'code')
        authorize.searchParams.set('scope', cfg.scopes.join(' '))
        authorize.searchParams.set('state', state)
        for (const [k, v] of Object.entries(cfg.extraAuthorizeParams ?? {})) {
          authorize.searchParams.set(k, v)
        }

        return Response.redirect(authorize.toString(), 302)
      },
    },
  },
})

// Allowlist: pivox:// for the native app's custom URL scheme, plus
// same-origin web returns. Anything else is rejected to prevent
// attackers from using the broker as an open redirector.
function isAllowedReturnUrl(candidate: string): boolean {
  try {
    const u = new URL(candidate)
    if (u.protocol === 'pivox:') return true
    const broker = process.env.OAUTH_BROKER_URL
    if (broker) {
      const brokerUrl = new URL(broker)
      if (u.origin === brokerUrl.origin) return true
    }
    return false
  } catch {
    return false
  }
}

function json(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
