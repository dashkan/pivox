import * as oidc from 'openid-client';

import { tokensFromResponse, type SessionTokens } from './tokens';

/**
 * Exchanges a refresh token for a fresh token set. Keycloak rotates refresh
 * tokens; when a deployment doesn't return a new one, the caller's existing
 * refresh token is preserved so the session stays refreshable.
 *
 * This is the pure protocol call only. Single-flight coordination (so a burst
 * of callers doesn't spend the same rotating refresh token twice and trip
 * Keycloak's reuse detection) is the caller's responsibility — it depends on
 * the caller's token store, which differs per host (Postgres row vs. in-memory).
 */
export async function refreshTokens(
  config: oidc.Configuration,
  refreshToken: string,
): Promise<SessionTokens> {
  const tokens = tokensFromResponse(await oidc.refreshTokenGrant(config, refreshToken));
  if (!tokens.refresh_token) tokens.refresh_token = refreshToken;
  return tokens;
}
