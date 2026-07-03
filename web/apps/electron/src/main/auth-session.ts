import {
  isTokenFresh,
  refreshTokens,
  type ConfigProvider,
  type OidcClaims,
  type SessionTokens,
} from '@pivox/oidc';

import type { AuthUser } from '../preload/auth-api';

/**
 * Durable storage for the refresh token. The production implementation is
 * safeStorage-backed (OS keychain) and lives in a separate module so this engine
 * has no `electron` import and stays unit-testable. Only the refresh token is
 * persisted — the access token stays in memory.
 */
export interface TokenPersistence {
  /** The stored refresh token, or null when there is none. */
  load(): string | null;
  save(refreshToken: string): void;
  clear(): void;
}

export interface AuthSessionOptions {
  /** Memoized OIDC config provider (public electron client). */
  config: ConfigProvider;
  persistence: TokenPersistence;
  /**
   * Decodes the id_token's claims. Injected so the engine stays testable; the
   * production decoder is `@pivox/oidc`'s `decodeIdTokenClaims` (a base64url JSON
   * parse — the token was verified by Keycloak at issuance and is re-verified by
   * the Cloud Controller on every API call).
   */
  decodeIdToken: (idToken: string) => OidcClaims | undefined;
  /** Scopes requested at login (includes offline_access for durable refresh). */
  scope: string;
}

/**
 * The electron main-process auth engine. Owns the token lifecycle for a single
 * signed-in user: holds the access token in memory, persists the refresh token,
 * refreshes on demand (single-flighted), and restores on boot.
 *
 * The single flight is the security-critical piece: a burst of renderer
 * getAccessToken() calls must not each spend the same rotating refresh token —
 * with Keycloak refresh-token rotation the later spends would trip reuse
 * detection and revoke the whole token family, forcing a mid-session logout.
 */
export class AuthSession {
  private tokens: SessionTokens | null = null;
  private inflight: Promise<SessionTokens> | null = null;

  constructor(private readonly options: AuthSessionOptions) {}

  /** Adopts a freshly-exchanged token set (from the login flow) and persists it. */
  setTokens(tokens: SessionTokens): void {
    this.tokens = tokens;
    if (tokens.refresh_token) this.options.persistence.save(tokens.refresh_token);
  }

  isAuthenticated(): boolean {
    return this.tokens !== null;
  }

  /** The current user derived from the id_token, or null when signed out / claimless. */
  getUser(): AuthUser | null {
    const idToken = this.tokens?.id_token;
    if (!idToken) return null;
    const claims = this.options.decodeIdToken(idToken);
    if (!claims?.sub) return null;
    return {
      id: claims.sub,
      email: claims.email,
      displayName: claims.name ?? claims.preferred_username,
      photoURL: claims.picture,
    };
  }

  /**
   * Returns a valid access token, refreshing (single-flighted) when the current
   * one is within the expiry skew. Throws when signed out or the refresh fails —
   * callers surface that as "please sign in again".
   */
  async getAccessToken(): Promise<string> {
    const current = this.tokens;
    if (current && isTokenFresh(current)) return current.access_token;
    const refreshToken = current?.refresh_token ?? this.options.persistence.load();
    if (!refreshToken) throw new Error('auth: not signed in');
    try {
      const refreshed = await this.refresh(refreshToken);
      return refreshed.access_token;
    } catch (err) {
      // A failed refresh means the session is no longer usable — a revoked token
      // family (reuse detection), an expired refresh token, or the IdP being
      // unreachable. Clear so the app transitions to signed-out rather than
      // sitting "authenticated" while every call 401s. (Trade-off: a transient
      // network blip signs the user out; a durable offline session would need
      // to classify the error, which the token endpoint doesn't always let us
      // do reliably — accepted for now.)
      this.clear();
      throw err;
    }
  }

  /**
   * Restores a session on boot from the persisted refresh token. Returns true if
   * a live session was re-established. A dead/expired refresh token is cleared so
   * a doomed token isn't retried on every launch.
   */
  async restore(): Promise<boolean> {
    if (this.tokens) return true;
    const refreshToken = this.options.persistence.load();
    if (!refreshToken) return false;
    try {
      await this.refresh(refreshToken);
      return true;
    } catch {
      this.clear();
      return false;
    }
  }

  /** Signs out locally: drops the in-memory tokens and the persisted refresh token. */
  clear(): void {
    this.tokens = null;
    this.options.persistence.clear();
  }

  /** The stored id_token, for an RP-initiated end-session hint. */
  idToken(): string | undefined {
    return this.tokens?.id_token;
  }

  private refresh(refreshToken: string): Promise<SessionTokens> {
    // Single flight: concurrent callers share one grant. A late caller that
    // arrives after a prior flight rotated the tokens re-checks freshness inside
    // the flight and returns the current set rather than spending again.
    if (this.inflight) return this.inflight;

    const promise = (async () => {
      const current = this.tokens;
      if (current && isTokenFresh(current)) return current;
      const config = await this.options.config();
      const tokens = await refreshTokens(config, refreshToken);
      this.tokens = tokens;
      if (tokens.refresh_token) this.options.persistence.save(tokens.refresh_token);
      return tokens;
    })();

    this.inflight = promise;
    return promise.finally(() => {
      this.inflight = null;
    });
  }
}
