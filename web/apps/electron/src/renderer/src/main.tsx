import './assets/main.css';

import { installErrorReporters } from '@pivox/observability';
import { QueryClient } from '@tanstack/react-query';
import {
  RouterProvider,
  createHashHistory,
  createRouter,
} from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { ensureFirebase } from './lib/firebase';
import { routeTree } from './routeTree.gen';

installErrorReporters();
ensureFirebase();
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
