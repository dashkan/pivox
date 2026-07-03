import * as oidc from 'openid-client';

/** Resolves the memoized OIDC {@link oidc.Configuration}. */
export type ConfigProvider = () => Promise<oidc.Configuration>;

export interface OidcClientOptions {
  /** Issuer URL, e.g. `https://kc.example/realms/pivox`. */
  issuer: string;
  clientId: string;
  /**
   * Confidential-client secret. Omit (or pass an empty string) for a public
   * (PKCE-only) client — the Electron desktop app, whose binary can't hold a
   * secret. Without a non-empty secret the client authenticates with `None`
   * (PKCE is the proof).
   */
  clientSecret?: string;
}

/**
 * Builds a lazily-memoized {@link ConfigProvider}. Discovery runs once on first
 * call; on failure the cached promise is cleared so the next call retries — a
 * transient IdP-down at first login must not poison the process for its whole
 * lifetime.
 *
 * Passing `clientSecret` uses openid-client's `client_secret` shorthand
 * (defaults to `client_secret_post`, which Keycloak's confidential
 * authenticator accepts). Omitting it selects `None` for a public client.
 */
export function createConfigProvider(options: OidcClientOptions): ConfigProvider {
  let configPromise: Promise<oidc.Configuration> | undefined;
  return () => {
    if (!configPromise) {
      const server = new URL(options.issuer);
      // Truthy check, not `!== undefined`: an empty-string secret must select
      // the public path, never confidential auth with an empty secret.
      const discovery = options.clientSecret
        ? oidc.discovery(server, options.clientId, options.clientSecret)
        : oidc.discovery(server, options.clientId, undefined, oidc.None());
      configPromise = discovery.catch((err: unknown) => {
        configPromise = undefined;
        throw err;
      });
    }
    return configPromise;
  };
}
