import * as oidc from 'openid-client'

/**
 * OIDC (Keycloak) client configuration for the `start` BFF.
 *
 * The BFF runs the OAuth Authorization Code + PKCE flow server-side using the
 * confidential `start` client. Discovery is memoized per process and lazy, so
 * server boot isn't coupled to Keycloak readiness — the first /auth/login pays
 * the discovery cost, by which point the IdP is reachable.
 */

const ISSUER = 'PIVOX_OIDC_ISSUER'
const CLIENT_ID = 'PIVOX_OIDC_CLIENT_ID'
const CLIENT_SECRET = 'PIVOX_OIDC_CLIENT_SECRET'

function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} is required for the OIDC BFF`)
  }
  return value
}

let configPromise: Promise<oidc.Configuration> | undefined

/**
 * Discovers and returns the memoized Keycloak {@link oidc.Configuration}. On
 * discovery failure the cached promise is cleared so the next call retries — a
 * transient IdP-down at first login shouldn't poison the process for its whole
 * lifetime.
 *
 * Passing the secret as the third argument is the `client_secret` shorthand and
 * defaults to client_secret_post (openid-client v6), which Keycloak's
 * confidential "Client Id and Secret" authenticator accepts.
 */
export function getOidcConfig(): Promise<oidc.Configuration> {
  if (!configPromise) {
    configPromise = oidc
      .discovery(new URL(required(ISSUER)), required(CLIENT_ID), required(CLIENT_SECRET))
      .catch((err: unknown) => {
        configPromise = undefined
        throw err
      })
  }
  return configPromise
}

/**
 * Scopes requested at login. `openid` is required for an ID token; `profile` and
 * `email` feed the backend's lazy provisioning from standard claims.
 */
export const OIDC_SCOPE = 'openid profile email'

/**
 * Builds the redirect_uri from the incoming request's origin, so it matches the
 * host the user actually reached us on (ngrok in dev, the app origin in prod).
 * Every origin used here MUST be a registered Valid Redirect URI on the `start`
 * client.
 */
export function callbackUrl(request: Request): URL {
  return new URL('/auth/callback', new URL(request.url).origin)
}
