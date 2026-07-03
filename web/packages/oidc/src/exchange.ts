import * as oidc from 'openid-client';

import { tokensFromResponse, type SessionTokens } from './tokens';

export interface ExchangeAuthorizationCodeOptions {
  /** The full callback URL as received, including `?code` and `?state`. */
  currentUrl: URL;
  /** The PKCE verifier retained from {@link buildAuthorizationRequest}. */
  codeVerifier: string;
  /** The state retained from {@link buildAuthorizationRequest}. */
  expectedState: string;
}

/**
 * Completes the Authorization Code + PKCE exchange. openid-client validates the
 * returned `state` against `expectedState` and proves possession with the PKCE
 * verifier, then this maps the response to {@link SessionTokens}. Rejects on any
 * validation or exchange failure — the caller treats that as a failed login.
 */
export async function exchangeAuthorizationCode(
  config: oidc.Configuration,
  options: ExchangeAuthorizationCodeOptions,
): Promise<SessionTokens> {
  const response = await oidc.authorizationCodeGrant(config, options.currentUrl, {
    pkceCodeVerifier: options.codeVerifier,
    expectedState: options.expectedState,
  });
  return tokensFromResponse(response);
}
