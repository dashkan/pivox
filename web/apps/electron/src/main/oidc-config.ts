import { createConfigProvider, type ConfigProvider } from '@pivox/oidc';

/**
 * OIDC (Keycloak) configuration for the Electron desktop app.
 *
 * Electron uses the PUBLIC `electron` client (PKCE, no secret — a desktop binary
 * can't hold one), in the same `pivox` realm the web app uses. Issuer + client id
 * come from VITE_* env (electron-vite exposes these to the main process via
 * import.meta.env), defaulting to the dev tunnel + realm.
 */

const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';
const ISSUER =
  import.meta.env.VITE_OIDC_ISSUER || `${BASE_URL.replace(/\/+$/, '')}/realms/pivox`;
const CLIENT_ID = import.meta.env.VITE_OIDC_CLIENT_ID || 'electron';

/**
 * Scopes requested at login. `offline_access` yields a durable refresh token so
 * the session survives app restarts (and outlives the Keycloak SSO session);
 * `profile`/`email` feed the id_token claims used for display + the backend's
 * lazy provisioning.
 */
export const OIDC_SCOPE = 'openid profile email offline_access';

/**
 * OIDC redirect_uri for the scheme transport: a branded HTTPS landing page
 * (served by the web app at `/launch`) that bounces the callback params into the
 * desktop app via the pivox:// scheme. Using an HTTPS redirect here — not
 * pivox:// directly — means the final browser screen is a real page rather than
 * a stranded OAuth tab, and keeps the token exchange on a standard https
 * redirect_uri. Must match the registered redirect URI on the Keycloak client.
 */
export const SCHEME_LANDING_URL = `${BASE_URL.replace(/\/+$/, '')}/launch`;

let provider: ConfigProvider | undefined;

/** Memoized discovery for the public electron client. */
export function oidcConfig(): ReturnType<ConfigProvider> {
  provider ??= createConfigProvider({ issuer: ISSUER, clientId: CLIENT_ID });
  return provider();
}
