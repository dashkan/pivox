/// <reference types="vite/client" />

// Augments Vite's ImportMetaEnv so `import.meta.env.VITE_*` reads are
// typed `string` instead of `any`. Keep in sync with the VITE_* vars
// the app actually reads.
interface ImportMetaEnv {
  // OTLP/HTTP traces endpoint for browser tracing (e.g. "/v1/traces", routed
  // through the ingress to the collector). Unset => tracing disabled. See router.tsx.
  readonly VITE_OTEL_TRACES_URL?: string;
}
