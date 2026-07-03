// Renderer-side OIDC-derived URLs. The main process owns the OIDC flow and
// tokens; the renderer only needs the account-console URL for "Manage Account".
// Derived from the same env the main process reads (see main/oidc-config.ts).
const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';
const ISSUER =
  import.meta.env.VITE_OIDC_ISSUER || `${BASE_URL.replace(/\/+$/, '')}/realms/pivox`;

/** Keycloak account console — `{issuer}/account`, opened in the system browser. */
export const ACCOUNT_CONSOLE_URL = `${ISSUER.replace(/\/+$/, '')}/account`;
