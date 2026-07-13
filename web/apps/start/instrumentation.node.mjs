// Server-side OpenTelemetry bootstrap for the TanStack Start SSR runtime.
//
// Loaded via `node --import ./instrumentation.node.mjs` (set as NODE_OPTIONS on
// the Aspire `start` resource) so OTel's HTTP + undici instrumentation is active
// BEFORE the Nitro/TanStack server handles a request or makes an SSR backend
// fetch. This version of TanStack Start (1.168 + Nitro) has no `server-entry`
// hook, so --import is the load-before-app mechanism.
//
// Config comes from env (Aspire injects it): OTEL_SERVICE_NAME=start and
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT (pointed at the ingress /v1/traces route,
// plaintext, so Node doesn't have to trust the dashboard's dev TLS cert).
// No-op when no OTLP endpoint is configured.
import { installNodeTracing } from '@pivox/observability/node';

installNodeTracing();
