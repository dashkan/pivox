import robotoLatin from '@fontsource-variable/roboto/files/roboto-latin-wght-normal.woff2?url';
import { TooltipProvider } from '@pivox/primitives/tooltip';
// Importing @pivox/storage at the top of __root.tsx ensures all item
// definitions in @pivox/storage/items have been evaluated (each
// `defineItem` self-registers) BEFORE buildBootScript() reads the
// registry. The route module is the earliest point in the load
// graph where the bootstrap script gets serialized.
import { buildBootScript } from '@pivox/storage';
import { QueryClient } from '@tanstack/react-query';
import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRouteWithContext,
} from '@tanstack/react-router';

import appCss from '../styles.css?url';

/**
 * Root route declares the router-context shape — the per-request
 * QueryClient lives here (constructed in `getRouter()` per call) so
 * route loaders can prefetch via `context.queryClient.prefetchQuery`
 * without sharing cache across SSR requests.
 *
 * `QueryClientProvider` is NOT in this tree — `setupRouterSsrQueryIntegration`
 * installs it via `router.options.Wrap`, so `useQuery` works anywhere
 * in the route tree without a manual provider here.
 */
export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Pivox' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      {
        rel: 'preload',
        href: robotoLatin,
        as: 'font',
        type: 'font/woff2',
        crossOrigin: 'anonymous',
      },
    ],
  }),
  component: RootComponent,
  shellComponent: RootDocument,
});

function RootComponent() {
  return (
    // TooltipProvider at the root so Radix Tooltip consumers
    // (currently SidebarMenuButton's tooltip prop in @pivox/ui/
    // app-shell, anywhere else later) find an ancestor context.
    // delay={0} matches shadcn's recommended sidebar shape —
    // the icon-collapsed sidebar relies on instant tooltips to be
    // navigable; the default 700ms feels broken.
    //
    // Auth context is NOT here: under the Keycloak BFF the user is
    // resolved server-side by the `_app` (and `/auth/create-org`)
    // gates, which wrap their subtree in `KeycloakAuthProvider`. The
    // root has no auth SDK and no client session to manage.
    <TooltipProvider delay={0}>
      <Outlet />
    </TooltipProvider>
  );
}

/**
 * Pre-hydration storage-item bootstrap. Inline so it runs before the
 * body paints. Generic over every registered StorageItem — for each:
 *   1. Read from the platform's selected backend. On http(s) origins
 *      (this app) that's the cookie; on file:// (electron) it's
 *      localStorage. The inline script branches on
 *      `location.protocol` — same selection logic as @pivox/storage's
 *      operations.ts. No cross-store promotion: each platform uses
 *      exactly one backend.
 *   2. Invoke the item's `onBoot` (if defined) with the parsed
 *      value, so the item can apply any DOM/runtime state that
 *      MUST exist before React mounts (e.g., theme's dark class).
 *
 * Themes don't get special-cased here — the THEME StorageItem
 * declares its own onBoot in `@pivox/storage` and the loop below
 * picks it up. Adding a new pre-mount setting is a matter of
 * defining its item with an onBoot; no changes here.
 */

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" dir="ltr" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: buildBootScript() }} />
        <HeadContent />
      </head>
      <body className="min-h-screen bg-background font-sans text-foreground antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
