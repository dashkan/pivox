/// <reference types="electron-vite/node" />

// Augments electron-vite's `ImportMetaEnv` for the main process. Keep
// in sync with the VITE_* vars main actually reads — currently just
// the Pivox app origin (see broker-auth.ts).
interface ImportMetaEnv {
  /** Pivox app origin — REST gateway + broker hooks + SPA. */
  readonly VITE_BASE_URL?: string;
}
