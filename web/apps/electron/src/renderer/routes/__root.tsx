import { TooltipProvider } from '@pivox/primitives/tooltip';
import { clearUserScopedItems } from '@pivox/storage';
import { KeycloakAuthProvider } from '@renderer/lib/keycloak-auth-provider';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Outlet, createRootRouteWithContext } from '@tanstack/react-router';

/**
 * Root route declares the router-context shape — the QueryClient
 * lives here (constructed once in `main.tsx`, per renderer process)
 * so route loaders can prefetch via `context.queryClient.prefetchQuery`
 * with the same shape the start app uses. Electron's renderer is
 * per-window by architecture, so the practical lifetime matches.
 */
export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
});

function RootComponent() {
  // `useRouteContext` is the idiomatic typed accessor for context
  // declared via `createRootRouteWithContext` — preferred over
  // `useRouter().options.context.queryClient` which mixes a runtime
  // config bag into the render path.
  const { queryClient } = Route.useRouteContext();
  return (
    // QueryClientProvider at the root so $api.useQuery (from
    // @pivox/client/react-query) works anywhere in the tree —
    // currently used by AppShellFeature for orgs + spaces. Pulls
    // the QueryClient from router context for symmetry with start;
    // see main.tsx for why a module-level singleton is fine in this
    // renderer-process context but the start app needs per-request.
    //
    // TooltipProvider at the root so Radix Tooltip consumers
    // (currently SidebarMenuButton's tooltip prop in @pivox/ui/
    // app-shell, anywhere else later) find an ancestor context.
    // delayDuration={0} matches shadcn's recommended sidebar shape.
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={0}>
        {/* Clear the outgoing user's state on sign-out so the next user
            doesn't inherit the selected org / cached org-list. No server
            session here (electron has no cookie) — just the client caches:
            user-scoped storage (localStorage) + the React Query cache. */}
        <KeycloakAuthProvider
          onBeforeSignOut={() => {
            clearUserScopedItems();
            queryClient.clear();
          }}
        >
          <div className="min-h-screen bg-background font-sans text-foreground antialiased">
            <Outlet />
          </div>
        </KeycloakAuthProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}
