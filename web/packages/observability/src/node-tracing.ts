import { diag, DiagConsoleLogger, DiagLogLevel } from '@opentelemetry/api';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-proto';
import { UndiciInstrumentation } from '@opentelemetry/instrumentation-undici';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { NodeSDK } from '@opentelemetry/sdk-node';
import { ATTR_SERVICE_NAME } from '@opentelemetry/semantic-conventions';

/**
 * Config for {@link installNodeTracing}. Everything else (endpoint, service
 * name) is read from the standard OTEL_* env, which the Aspire AppHost injects.
 */
export interface NodeTracingConfig {
  /** Fallback service.name, used only when OTEL_SERVICE_NAME isn't set. */
  serviceName?: string;
  /**
   * Full OTLP/HTTP traces endpoint (".../v1/traces"). Overrides
   * OTEL_EXPORTER_OTLP_TRACES_ENDPOINT. Falsy => fall back to env.
   */
  otlpTracesUrl?: string;
}

// Guard against double-install on a single process. Keyed on globalThis (not a
// module-local boolean) because `node --import` can evaluate the module graph
// more than once (loader vs main realm), producing separate module instances
// that wouldn't share a module-local flag — each would start its own SDK.
const STARTED = Symbol.for('pivox.nodeTracingStarted');

/**
 * Install Node-side OpenTelemetry tracing for the TanStack Start SSR runtime.
 * Traces outgoing `fetch`/undici calls — the SSR prefetch to the backend — and
 * propagates W3C trace context, giving SSR -> api -> db.
 *
 * MUST run before app code so undici is instrumented first; load via
 * `node --import ./instrumentation.node.mjs` (this TanStack Start version has no
 * `server-entry` hook).
 *
 * Deliberately undici-only. `instrumentation-http` would also fire — but in the
 * Vite dev server it traces every dev-module fetch (`/@fs`, `/node_modules/.vite`)
 * and asset request, which is pure dev noise. undici captures the openapi-fetch
 * backend calls, which is the point.
 *
 * No-op when no OTLP endpoint is configured / the SDK is disabled; idempotent.
 * Set PIVOX_OTEL_DEBUG=1 for verbose SDK logging.
 */
export function installNodeTracing(config: NodeTracingConfig = {}): void {
  // The started flag lives on globalThis under a `Symbol.for` key so tracing
  // dedups across duplicate module copies (multiple bundles / realms).
  // globalThis has no symbol index signature in lib.dom, and a global
  // augmentation for a computed Symbol.for key isn't expressible, so this global
  // boundary is asserted.
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- globalThis has no symbol index signature; augmenting it for a computed Symbol.for key is impractical
  const globals = globalThis as Record<symbol, boolean>;
  if (globals[STARTED] || isTruthy(process.env.OTEL_SDK_DISABLED)) return;

  const tracesUrl =
    config.otlpTracesUrl ?? process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT;
  if (!tracesUrl && !process.env.OTEL_EXPORTER_OTLP_ENDPOINT) return; // disabled
  globals[STARTED] = true;

  if (isTruthy(process.env.PIVOX_OTEL_DEBUG)) {
    diag.setLogger(new DiagConsoleLogger(), DiagLogLevel.DEBUG);
  }

  try {
    const sdk = new NodeSDK({
      // When OTEL_SERVICE_NAME is set (Aspire injects it), NodeSDK's env detector
      // already picks it up, so only supply a resource for the fallback name.
      resource:
        config.serviceName && !process.env.OTEL_SERVICE_NAME
          ? resourceFromAttributes({ [ATTR_SERVICE_NAME]: config.serviceName })
          : undefined,
      // OTLP/HTTP + protobuf. A traces-specific URL is used verbatim — we point
      // it at the ingress /v1/traces route (plaintext) so Node never has to trust
      // the dashboard's dev TLS cert (which Go trusts via the system store but
      // Node's TLS/grpc-js does not). Without a URL it reads
      // OTEL_EXPORTER_OTLP_ENDPOINT (the exporter appends /v1/traces).
      traceExporter: new OTLPTraceExporter(tracesUrl ? { url: tracesUrl } : {}),
      instrumentations: [
        new UndiciInstrumentation({
          // Name SSR spans "SSR GET /v1/.../spaces" (route, query stripped) —
          // the undici default is just "GET". The "SSR" prefix distinguishes
          // them from the browser/CSR fetch spans (prefixed "CSR") in the same
          // `start` resource.
          requestHook: (span, request) => {
            const path = request.path.split('?')[0];
            span.updateName(`SSR ${request.method}${path ? ` ${path}` : ''}`);
          },
        }),
      ],
    });

    sdk.start();

    // Flush buffered spans on shutdown. The BatchSpanProcessor buffers, so a
    // bare exit drops the last batch: beforeExit covers a clean exit, and
    // SIGTERM/SIGINT cover Aspire stopping the dev server.
    const shutdown = (): void => {
      void sdk.shutdown().catch(() => undefined);
    };
    process.once('SIGTERM', shutdown);
    process.once('SIGINT', shutdown);
    process.once('beforeExit', shutdown);
  } catch (err) {
    // This runs before app code (via --import), so a tracing misconfig must not
    // take down the server. Mirrors the web path's reportError safety stance.
    console.error('[pivox-otel] failed to start node tracing:', err);
  }
}

function isTruthy(v: string | undefined): boolean {
  return v != null && (v.toLowerCase() === 'true' || v === '1');
}
