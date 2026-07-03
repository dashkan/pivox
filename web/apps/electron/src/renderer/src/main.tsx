import './assets/main.css';

import { installErrorReporters, installWebTracing } from '@pivox/observability';
import { QueryClient } from '@tanstack/react-query';
import {
  RouterProvider,
  createHashHistory,
  createRouter,
} from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { routeTree } from './routeTree.gen';

installErrorReporters();

// Browser OpenTelemetry tracing. Unlike the `start` app, no Aspire resource
// injects VITE_OTEL_TRACES_URL here, and the renderer origin (localhost:5173 in
// dev) is cross-origin to the backend — relative URLs can't resolve. So in DEV
// default the OTLP endpoint to the backend ingress's /v1/traces (envoy routes it
// to the collector). VITE_OTEL_TRACES_URL always wins; a PRODUCTION build with
// no override stays a no-op, so packaged apps never auto-ship spans to a dev
// tunnel.
const backendBaseUrl = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';
installWebTracing({
  // Electron isn't an Aspire resource, but keep the bare-name convention
  // (start / electron) consistent with the Aspire-named services.
  serviceName: 'electron',
  otlpTracesUrl:
    import.meta.env.VITE_OTEL_TRACES_URL ??
    (import.meta.env.DEV ? `${backendBaseUrl}/v1/traces` : undefined),
  // Cross-origin to the backend → propagate traceparent (CORS must allow it).
  propagateTraceHeaderCorsUrls: [backendBaseUrl],
});

const hashHistory = createHashHistory();

// One QueryClient per renderer process — Electron has no SSR, so a
// module-level instance is per-window/per-user by architecture (each
// renderer is its own process with its own JS heap). We still pass it
// through router context for symmetry with the start app and so route
// loaders can use `context.queryClient.prefetchQuery(...)` the same
// way. NO `routerWithQueryClient` wrapper here — that's an SSR
// dehydrate/hydrate helper and would only add bundle weight in a
// non-SSR build.
const queryClient = new QueryClient();
const router = createRouter({
  routeTree,
  history: hashHistory,
  context: { queryClient },
});

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

const rootEl = document.getElementById('root');
if (!rootEl) {
  throw new Error('Root element #root not found in index.html');
}

createRoot(rootEl).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);
