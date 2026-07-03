/**
 * Framework-agnostic OpenID Connect (Keycloak) core over openid-client.
 *
 * Shared by the `start` BFF (server-side, confidential client, Postgres-backed
 * sessions) and the Electron main process (public PKCE client, safeStorage-backed
 * tokens). This package owns only the protocol mechanics — discovery, PKCE
 * authorization, code exchange, refresh, and end-session. Storage and
 * single-flight coordination live in each host, because they differ materially.
 */
export { createConfigProvider } from './config';
export type { ConfigProvider, OidcClientOptions } from './config';

export { buildAuthorizationRequest } from './authorize';
export type { AuthorizationRequest, BuildAuthorizationRequestOptions } from './authorize';

export { exchangeAuthorizationCode } from './exchange';
export type { ExchangeAuthorizationCodeOptions } from './exchange';

export { refreshTokens } from './refresh';

export { buildEndSessionUrl } from './logout';
export type { BuildEndSessionUrlOptions } from './logout';

export { EXPIRY_SKEW_MS, isTokenFresh, tokensFromResponse } from './tokens';
export type { SessionTokens } from './tokens';

export { decodeIdTokenClaims } from './claims';
export type { OidcClaims } from './claims';
