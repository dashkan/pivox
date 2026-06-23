/// <reference types="vite/client" />

// Augments Vite's ImportMetaEnv so `import.meta.env.VITE_*` reads are
// typed `string` instead of `any`. Keep in sync with the VITE_* vars
// the app actually reads (see lib/firebase.ts, lib/auth-providers.ts).
interface ImportMetaEnv {
  readonly VITE_FIREBASE_API_KEY: string;
  readonly VITE_FIREBASE_AUTH_DOMAIN: string;
  readonly VITE_FIREBASE_PROJECT_ID: string;
  readonly VITE_FIREBASE_STORAGE_BUCKET: string;
  readonly VITE_FIREBASE_MESSAGING_SENDER_ID: string;
  readonly VITE_FIREBASE_APP_ID: string;
  readonly VITE_AUTH_PROVIDERS?: string;
  // OTLP/HTTP traces endpoint for browser tracing (e.g. "/v1/traces", routed
  // through envoy to the collector). Unset => tracing disabled. See router.tsx.
  readonly VITE_OTEL_TRACES_URL?: string;
}
