import { createRouter as createTanStackRouter } from '@tanstack/react-router';

import { ensureFirebase } from './lib/firebase';
import { routeTree } from './routeTree.gen';

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
