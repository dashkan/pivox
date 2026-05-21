import { installErrorReporters } from '@pivox/observability';
import { createRouter as createTanStackRouter } from '@tanstack/react-router';

import { ensureFirebase } from './lib/firebase';
import { routeTree } from './routeTree.gen';

// Install global error reporters at module load. installErrorReporters
// is SSR-guarded (no-ops without `window`), so this is a client-only
// effect in practice even though router.tsx is imported on the server too.
installErrorReporters();

export function getRouter() {
  ensureFirebase();
  const router = createTanStackRouter({
    routeTree,

    scrollRestoration: true,
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
  });

  return router;
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
