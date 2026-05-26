import { installErrorReporters } from '@pivox/observability';
import { QueryClient } from '@tanstack/react-query';
import { createRouter as createTanStackRouter } from '@tanstack/react-router';
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query';

import { ensureFirebase } from './lib/firebase';
import { routeTree } from './routeTree.gen';

// Install global error reporters at module load. installErrorReporters
// is SSR-guarded (no-ops without `window`), so this is a client-only
// effect in practice even though router.tsx is imported on the server too.
installErrorReporters();

/**
 * Build a router with a fresh, per-call QueryClient wired into
 * router context.
 *
 * TanStack Start calls `getRouter()` once per SSR request on the
 * server, and once per page load on the client. Constructing the
 * QueryClient here yields a per-request cache on the server (no
 * cross-user cache leak) and a per-tab cache on the client
 * (long-lived, as expected).
 *
 * `setupRouterSsrQueryIntegration` wires the dehydrate/hydrate
 * boundary so a route loader's `context.queryClient.prefetchQuery(...)`
 * is serialized into the SSR payload and rehydrated on the client
 * without a refetch round-trip. By default it also installs a
 * `QueryClientProvider` via `router.options.Wrap`, so consumers can
 * call `useQuery` anywhere in the route tree without an explicit
 * provider in `__root.tsx`.
 *
 * Do NOT replace the per-call construction with a module-level
 * `new QueryClient()` — that singleton would be shared across
 * concurrent SSR requests and leak one user's cached data to
 * another the moment a loader starts prefetching.
 */
export function getRouter() {
  ensureFirebase();
  const queryClient = new QueryClient();
  const router = createTanStackRouter({
    routeTree,
    context: { queryClient },
    scrollRestoration: true,
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
  });
  setupRouterSsrQueryIntegration({ router, queryClient });
  return router;
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
