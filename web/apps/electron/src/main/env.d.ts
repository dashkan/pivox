/// <reference types="electron-vite/node" />

// Augments electron-vite's `ImportMetaEnv` for the main process. Keep
// in sync with the VITE_* vars main actually reads (see oidc-config.ts).
interface ImportMetaEnv {
  /** Pivox app origin — REST gateway + SPA. */
  readonly VITE_BASE_URL?: string;
  /** OIDC issuer, e.g. https://pivox.example/realms/pivox. Defaults from VITE_BASE_URL. */
  readonly VITE_OIDC_ISSUER?: string;
  /** Public Keycloak client id for the desktop app. Defaults to "electron". */
  readonly VITE_OIDC_CLIENT_ID?: string;
}
