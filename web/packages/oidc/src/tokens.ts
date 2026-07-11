import type { TokenEndpointResponse } from 'openid-client';

/**
 * The token set both wrappers persist. JSON-safe by construction
 * (string/number/optional fields only) so it round-trips through Postgres jsonb
 * (the start BFF) or an encrypted blob on disk (the Electron main process)
 * without a codec.
 */
export type SessionTokens = {
  access_token: string;
  refresh_token?: string;
  id_token?: string;
  /** Epoch milliseconds at which the access token expires. */
  expires_at: number;
};

/** Refresh the access token when it's within this window of expiry. */
export const EXPIRY_SKEW_MS = 30_000;

/**
 * Maps an openid-client token response to {@link SessionTokens}, converting the
 * relative `expires_in` into an absolute `expires_at`. Keycloak always returns
 * `expires_in`; the 300s fallback only guards a spec-incomplete IdP.
 */
export function tokensFromResponse(response: TokenEndpointResponse): SessionTokens {
  const expiresInSeconds = response.expires_in ?? 300;
  return {
    access_token: response.access_token,
    refresh_token: response.refresh_token,
    id_token: response.id_token,
    expires_at: Date.now() + expiresInSeconds * 1000,
  };
}

/**
 * True when the access token is still valid beyond the refresh skew — i.e. it
 * can be used as-is without a refresh. Callers gate refresh on the negation.
 */
export function isTokenFresh(
  tokens: Pick<SessionTokens, 'expires_at'>,
  skewMs: number = EXPIRY_SKEW_MS,
): boolean {
  return tokens.expires_at - Date.now() >= skewMs;
}
