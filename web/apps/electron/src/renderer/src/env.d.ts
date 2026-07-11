/// <reference types="vite/client" />

// Augments Vite's ImportMetaEnv so `import.meta.env.VITE_*` reads are
// typed `string` instead of `any`. Keep in sync with the VITE_* vars
// the renderer actually reads (see lib/oidc-env.ts, lib/api-client.ts).
interface ImportMetaEnv {
  /** Pivox app origin — REST gateway + SPA. */
  readonly VITE_BASE_URL?: string;
  /** OIDC issuer, e.g. https://pivox.example/realms/pivox. Defaults from VITE_BASE_URL. */
  readonly VITE_OIDC_ISSUER?: string;
  // Absolute OTLP/HTTP traces endpoint for browser tracing (e.g.
  // "https://pivox.example/v1/traces"). Unset => tracing disabled. Must be
  // absolute — the renderer origin is app://, so relative URLs can't resolve.
  readonly VITE_OTEL_TRACES_URL?: string;
}
