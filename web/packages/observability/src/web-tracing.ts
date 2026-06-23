import { reportError } from './report';

/**
 * Config for {@link installWebTracing}. Each app supplies its own identity +
 * endpoint; the wiring is shared.
 */
export interface WebTracingConfig {
  /** service.name for emitted spans, e.g. "pivox-start" | "pivox-electron". */
  serviceName: string;
  /**
   * OTLP/HTTP traces endpoint. Absolute (`https://host/v1/traces`) or
   * origin-relative (`/v1/traces`, resolved against `window.location.origin`).
   * Falsy => tracing is disabled (no-op). Apps typically read this from a
   * build-time env var so it stays off unless configured.
   */
  otlpTracesUrl?: string;
  /**
   * URLs the fetch/XHR instrumentation should inject the W3C `traceparent`
   * header into. Same-origin requests are always propagated; list the
   * cross-origin backend origin(s) here (e.g. an Electron renderer calling a
   * remote API). The backend must allow `traceparent`/`tracestate` via CORS.
   *
   * String entries are treated as URL **prefixes** (a base URL matches every
   * path under it) — NOT OTel's default exact-string match, which never matches
   * a real request URL with a path. Pass a RegExp for full control.
   */
  propagateTraceHeaderCorsUrls?: (string | RegExp)[];
  /** Extra resource attributes merged onto the span resource. */
  resourceAttributes?: Record<string, string>;
}

let installed = false;

/**
 * Install browser OpenTelemetry tracing: a WebTracerProvider exporting spans
 * over OTLP/HTTP, with fetch/XHR/document-load auto-instrumentation that
 * propagates W3C trace context to the backend — so a browser request continues
 * into the cloud's distributed trace (browser -> envoy -> api -> db).
 *
 * Shared by the start (TanStack) and electron (renderer) apps; call once,
 * client-side, at startup (next to installErrorReporters).
 *
 * No-op under SSR (no `window`), when `otlpTracesUrl` is unset, or if already
 * installed. The OpenTelemetry SDK is dynamically imported so it stays out of
 * the SSR bundle and the initial client chunk — it loads only when tracing is
 * actually enabled.
 */
export function installWebTracing(config: WebTracingConfig): void {
  if (typeof window === 'undefined' || !config.otlpTracesUrl || installed) {
    return;
  }
  installed = true;
  // Fire-and-forget: the dynamic import resolves client-side only. Kept sync so
  // call sites mirror installErrorReporters().
  void setupWebTracing(config);
}

async function setupWebTracing(config: WebTracingConfig): Promise<void> {
  try {
    const [
      { WebTracerProvider, BatchSpanProcessor, StackContextManager },
      { registerInstrumentations },
      { getWebAutoInstrumentations },
      { OTLPTraceExporter },
      { resourceFromAttributes },
      { ATTR_SERVICE_NAME },
    ] = await Promise.all([
      import('@opentelemetry/sdk-trace-web'),
      import('@opentelemetry/instrumentation'),
      import('@opentelemetry/auto-instrumentations-web'),
      import('@opentelemetry/exporter-trace-otlp-http'),
      import('@opentelemetry/resources'),
      import('@opentelemetry/semantic-conventions'),
    ]);

    // `otlpTracesUrl` is guaranteed non-empty by installWebTracing.
    const url = resolveTracesUrl(config.otlpTracesUrl as string);

    const provider = new WebTracerProvider({
      resource: resourceFromAttributes({
        [ATTR_SERVICE_NAME]: config.serviceName,
        ...config.resourceAttributes,
      }),
      spanProcessors: [new BatchSpanProcessor(new OTLPTraceExporter({ url }))],
    });
    // StackContextManager (not ZoneContextManager) — no zone.js, which
    // monkeypatches global async primitives and is risky under React 19's
    // scheduler. Fetch instrumentation captures the active context at call
    // time, so backend propagation works without zone-based async tracking.
    provider.register({ contextManager: new StackContextManager() });

    // OTel matches plain-string propagate-urls EXACTLY (url === pattern), so a
    // base URL like "https://api.example.com" never matches a real request URL
    // with a path — and cross-origin traceparent is silently never injected.
    // Convert strings to URL-prefix regexes so a config string means "this
    // origin/prefix and everything under it" (the intuitive behavior). Same-
    // origin requests always propagate regardless (OTel short-circuits those).
    const propagateUrls = (config.propagateTraceHeaderCorsUrls ?? []).map(
      toPrefixMatcher,
    );

    registerInstrumentations({
      instrumentations: getWebAutoInstrumentations({
        '@opentelemetry/instrumentation-fetch': {
          propagateTraceHeaderCorsUrls: propagateUrls,
          // Rename "HTTP GET" -> "CSR GET /path" so the route is visible in the
          // trace list and browser (CSR) spans are distinguishable from the
          // server (SSR) undici spans that share the `start` resource.
          applyCustomAttributesOnSpan: (span, request, result) => {
            const url =
              result instanceof Response
                ? result.url
                : request instanceof Request
                  ? request.url
                  : '';
            const method =
              request instanceof Request
                ? request.method
                : (request.method ?? 'GET');
            const name = httpSpanName(url, method);
            if (name) span.updateName(`CSR ${name}`);
          },
        },
        '@opentelemetry/instrumentation-xml-http-request': {
          propagateTraceHeaderCorsUrls: propagateUrls,
          applyCustomAttributesOnSpan: (span, xhr) => {
            // XHR exposes the resolved URL but not the method; path-only name.
            const name = httpSpanName(xhr.responseURL);
            if (name) span.updateName(`CSR ${name}`);
          },
        },
        // Click/keypress spans are noise for backend correlation.
        '@opentelemetry/instrumentation-user-interaction': { enabled: false },
      }),
    });
  } catch (err) {
    reportError(err, { source: 'installWebTracing' });
  }
}

/**
 * Resolve an origin-relative traces URL to absolute (the OTLP exporter wants an
 * absolute URL). Absolute inputs pass through unchanged.
 */
function resolveTracesUrl(url: string): string {
  if (/^https?:\/\//i.test(url)) return url;
  return new URL(url, window.location.origin).toString();
}

/**
 * Builds an HTTP span name like "GET /v1/organizations/acme/spaces" (or just
 * the path when method is unknown) so the route shows in the trace list. Query
 * strings are dropped. Returns undefined for an unparseable/empty URL.
 *
 * Note: the path keeps real IDs, so span-name cardinality is high — fine for
 * dev observability; templatize (`/v1/organizations/{org}/spaces`) if this is
 * ever pointed at a cardinality-sensitive prod backend.
 */
/**
 * Converts a propagate-url entry to a regex. RegExp passes through; a string
 * becomes an anchored prefix match (`^<escaped>`), so "https://host" matches
 * "https://host/any/path" — unlike OTel's exact string match.
 */
function toPrefixMatcher(entry: string | RegExp): RegExp {
  if (entry instanceof RegExp) return entry;
  // Anchor at start AND require an origin/path boundary after the prefix, so a
  // base origin doesn't over-match a sibling host: "https://host" must match
  // "https://host/..." and "https://host:443/..." but NOT "https://host.evil.com"
  // (which would otherwise leak traceparent cross-origin). Trailing slashes are
  // stripped first so "https://host/" behaves the same as "https://host".
  const escaped = entry
    .replace(/\/+$/, '')
    .replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp('^' + escaped + '(?:[/?#:]|$)');
}

function httpSpanName(url: string, method?: string): string | undefined {
  if (!url) return undefined;
  try {
    const path = new URL(url, window.location.origin).pathname;
    return method ? `${method.toUpperCase()} ${path}` : path;
  } catch {
    return undefined;
  }
}
