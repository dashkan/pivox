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
  // Default `staleTime` of 1 minute on every query.
  //
  // Per the TanStack Query SSR docs: with SSR, set a default
  // staleTime above 0 to avoid refetching immediately on the client.
  // SSR's `setQueryData` records `dataUpdatedAt = Date.now()`, that
  // timestamp is dehydrated into the SSR payload and rehydrated by
  // setupRouterSsrQueryIntegration on the client. useQuery on mount
  // compares `Date.now() - dataUpdatedAt` to `staleTime`: under, no
  // refetch; over, background refetch (stale-while-revalidate).
  //
  // 1 minute keeps SSR-fresh data authoritative for the cold-load
  // window without blocking long-lived tabs from picking up changes
  // — for an open tab, focus/reconnect events still revalidate
  // after the window. Per-query overrides via the `staleTime` option
  // still win where a specific call has different freshness needs
  // (e.g., a real-time feed wanting staleTime: 0, or truly immutable
  // data wanting Infinity).
  //
  // Mutations force-refresh the data they touched via explicit
  // `queryClient.invalidateQueries({ queryKey: ... })` in
  // `onSettled` — that's the path that keeps the UI in sync after
  // writes, independent of the time-based revalidation.
  //
  // Electron deliberately runs the default `staleTime: 0`. With no
  // SSR-primed cache there, refetch-on-mount gives users live data
  // on every screen entry — the right default for a desktop app.
  // The 60s window only earns its keep when there's a dehydrated
  // payload to trust.
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 60 * 1000 },
    },
  });
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
