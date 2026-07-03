import * as oidc from 'openid-client';

export interface BuildEndSessionUrlOptions {
  /** Where Keycloak returns the user after ending the session. */
  postLogoutRedirectUri?: string;
  /** The stored id_token, so Keycloak can skip the logout confirmation prompt. */
  idTokenHint?: string;
}

/**
 * Builds Keycloak's RP-initiated end-session URL, so signing out of the app also
 * terminates the IdP session. Both params are optional: without an id_token_hint
 * Keycloak may show a confirmation prompt; without a redirect it lands on its own
 * logged-out page.
 */
export function buildEndSessionUrl(
  config: oidc.Configuration,
  options: BuildEndSessionUrlOptions = {},
): URL {
  return oidc.buildEndSessionUrl(config, {
    ...(options.postLogoutRedirectUri
      ? { post_logout_redirect_uri: options.postLogoutRedirectUri }
      : {}),
    ...(options.idTokenHint ? { id_token_hint: options.idTokenHint } : {}),
  });
}
