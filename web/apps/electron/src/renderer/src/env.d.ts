/// <reference types="vite/client" />

// Augments Vite's ImportMetaEnv so `import.meta.env.VITE_*` reads are
// typed `string` instead of `any`. Keep in sync with the VITE_* vars
// the renderer actually reads (see lib/firebase.ts, lib/auth-providers.ts).
interface ImportMetaEnv {
  readonly VITE_FIREBASE_API_KEY: string;
  readonly VITE_FIREBASE_AUTH_DOMAIN: string;
  readonly VITE_FIREBASE_PROJECT_ID: string;
  readonly VITE_AUTH_PROVIDERS?: string;
  /** Pivox app origin — REST gateway + broker hooks + SPA. */
  readonly VITE_BASE_URL?: string;
  // Absolute OTLP/HTTP traces endpoint for browser tracing (e.g.
  // "https://pivox.ngrok.app/v1/traces"). Unset => tracing disabled. Must be
  // absolute — the renderer origin is app://, so relative URLs can't resolve.
  readonly VITE_OTEL_TRACES_URL?: string;
}
