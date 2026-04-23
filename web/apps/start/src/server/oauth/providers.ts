/**
 * Static OAuth provider registry. Each entry bundles the provider-
 * specific URLs, env-backed credentials, default scopes, and — most
 * importantly — a `toFirebaseCredential` adapter that normalizes
 * whatever the provider returns (access_token for GitHub, id_token
 * for Google/Apple/OIDC) into a Firebase sign-in input the callback
 * can hand to Firebase Admin.
 *
 * The routes never import this directly; they go through
 * `resolver.resolveProvider(name)` so dynamic (DB-backed) providers
 * — enterprise SSO — can be added later without touching route code.
 *
 * To add a provider:
 *   1. Add env vars to .envrc (or .envrc.<mode>) — server-only, no
 *      VITE_ prefix.
 *   2. Add an entry here.
 *   3. Register the broker's callback URL in the provider's OAuth
 *      app console.
 *
 * Apple lives here too once implemented, but it's more complex:
 *   - Uses JWT client secret (signed with a private key, rotated
 *     every 6 months).
 *   - Emits id_token that must be verified against Apple's JWKS.
 *   - Name/email only returned on first authorization; subsequent
 *     sign-ins omit them.
 *   - See: https://developer.apple.com/documentation/sign_in_with_apple
 */

export type FirebaseCredentialInput =
  | { kind: 'github_access_token'; accessToken: string }
  | { kind: 'oidc_id_token'; provider: string; idToken: string; accessToken?: string }

export interface TokenExchangeResponse {
  access_token?: string
  id_token?: string
  token_type?: string
  scope?: string
  [k: string]: unknown
}

export interface ProviderConfig {
  /** Identifier used in routes (`/api/oauth/<id>/start`). */
  id: string
  /** Provider's OAuth authorization endpoint. */
  authorizeUrl: string
  /** Provider's token exchange endpoint. */
  tokenUrl: string
  /** Default scopes requested on authorize. */
  scopes: string[]
  /** Extra query params to send on the authorize request. */
  extraAuthorizeParams?: Record<string, string>
  /** Client ID — public, sent in authorize URL. */
  clientId: string
  /** Client secret — server-only, used in token exchange. */
  clientSecret: string
  /** Additional headers for the token exchange POST. */
  tokenRequestHeaders?: Record<string, string>
  /**
   * Adapts the provider's token response into the shape Firebase
   * Admin can consume. Called after successful exchange in the
   * callback route.
   */
  toFirebaseCredential(resp: TokenExchangeResponse): FirebaseCredentialInput
}

// Helper: fail loud at startup if a required env var is missing.
// (Actually we only fail at the call site to avoid breaking boot
// when a provider's vars aren't set locally — see `resolver.ts`.)
function env(name: string): string {
  return process.env[name] ?? ''
}

export const staticProviders: Record<string, ProviderConfig> = {
  github: {
    id: 'github',
    authorizeUrl: 'https://github.com/login/oauth/authorize',
    tokenUrl: 'https://github.com/login/oauth/access_token',
    scopes: ['read:user', 'user:email'],
    extraAuthorizeParams: { allow_signup: 'true' },
    clientId: env('GITHUB_CLIENT_ID'),
    clientSecret: env('GITHUB_CLIENT_SECRET'),
    tokenRequestHeaders: { Accept: 'application/json' },
    toFirebaseCredential(resp) {
      if (!resp.access_token) throw new Error('GitHub token exchange missing access_token')
      return { kind: 'github_access_token', accessToken: resp.access_token }
    },
  },

  // google: NOT in the broker.
  //   Google uses the native Google Sign-In SDK (GIDSignIn) on Apple
  //   platforms with the URL-scheme intercept pattern:
  //     - Google treats our iOS/macOS app as a public OAuth client.
  //     - client_id is registered in Info.plist as a reversed-DNS URL
  //       scheme; there is no client_secret.
  //     - The SDK hands back an id_token via that scheme; no server
  //       round-trip, no redirect_uri registered with Google.
  //   On the web, Google auth goes through Firebase's
  //   GoogleAuthProvider popup/redirect — also doesn't route here.
  //   This broker only exists for providers that REQUIRE a
  //   server-mediated callback (GitHub's single-redirect-URL limit,
  //   enterprise SSO, Apple's JWT-signed client secret).

  // apple: TODO.
  //   Needs:
  //     - ES256-signed JWT as client_secret, regenerated every ≤6 months.
  //     - id_token verification against appleid.apple.com/auth/keys (JWKS).
  //     - Handling of the first-sign-in-only name/email payload in the
  //       authorize redirect (form_post response_mode).
  //   When we add it, it'll fit this same ProviderConfig interface —
  //   the `clientSecret` becomes a dynamically-generated JWT inside
  //   `toFirebaseCredential` (or we widen the interface to support a
  //   `buildClientSecret()` hook).
}
