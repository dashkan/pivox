import * as oidc from 'openid-client';

export interface AuthorizationRequest {
  /** The Keycloak authorization URL to open in the system browser. */
  authorizationUrl: string;
  /** PKCE verifier — kept private by the caller until the code exchange. */
  codeVerifier: string;
  /** CSRF state — echoed back on the callback and verified there. */
  state: string;
}

export interface BuildAuthorizationRequestOptions {
  /** Registered redirect URI (loopback or custom scheme for a native app). */
  redirectUri: string;
  /** Space-delimited scopes, e.g. `openid profile email offline_access`. */
  scope: string;
  /** Extra authorization params (e.g. `login_hint`, `prompt`). */
  extraParams?: Record<string, string>;
}

/**
 * Builds an Authorization Code + PKCE (S256) request: generates the verifier,
 * challenge, and state, and returns the URL to open plus the verifier + state
 * the caller must retain to complete {@link exchangeAuthorizationCode}.
 */
export async function buildAuthorizationRequest(
  config: oidc.Configuration,
  options: BuildAuthorizationRequestOptions,
): Promise<AuthorizationRequest> {
  const codeVerifier = oidc.randomPKCECodeVerifier();
  const codeChallenge = await oidc.calculatePKCECodeChallenge(codeVerifier);
  const state = oidc.randomState();

  const authorizationUrl = oidc.buildAuthorizationUrl(config, {
    // extraParams first so protocol-critical params below always win — a caller
    // must not be able to override the generated state / PKCE method (the
    // function returns the generated state; an overridden URL state would
    // silently fail the callback's expectedState check).
    ...options.extraParams,
    redirect_uri: options.redirectUri,
    scope: options.scope,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
    state,
  });

  return { authorizationUrl: authorizationUrl.href, codeVerifier, state };
}
