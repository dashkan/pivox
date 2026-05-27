import { AuthProvider } from '@pivox/features/auth';
import { TooltipProvider } from '@pivox/primitives/tooltip';
import { QueryClient } from '@tanstack/react-query';
import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRouteWithContext,
  useRouter,
} from '@tanstack/react-router';

import appCss from '../styles.css?url';

import { clearSession, createSession } from '@/server/auth-session';

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
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  component: RootComponent,
  shellComponent: RootDocument,
});

function RootComponent() {
  const router = useRouter();
  return (
    // TooltipProvider at the root so Radix Tooltip consumers
    // (currently SidebarMenuButton's tooltip prop in @pivox/ui/
    // app-shell, anywhere else later) find an ancestor context.
    // delayDuration={0} matches shadcn's recommended sidebar shape —
    // the icon-collapsed sidebar relies on instant tooltips to be
    // navigable; the default 700ms feels broken.
    <TooltipProvider delayDuration={0}>
      <AuthProvider
        onBeforeSignOut={() => clearSession()}
        // Proactive cookie refresh — Firebase rotates the ID token
        // every ~55 min while the app is open; each rotation re-mints
        // the cookie so the 14-day window slides forward continuously.
        // An actively-used app never sees cookie expiry; inactivity
        // beyond 14 days falls through to the verify-session interim
        // recovery flow on the next visit.
        onTokenRefresh={(idToken) => createSession({ data: { idToken } })}
        // Post-sign-out redirect. `_app`'s `beforeLoad` only runs on
        // navigation events, so without an explicit navigate the user
        // stays on the current authenticated route with a null user
        // and the SSR-rendered content still on screen. Electron uses
        // AuthGateFeature for the same purpose; start uses this hook
        // since the server-side gate replaces AuthGateFeature.
        onSignedOut={() => router.navigate({ to: '/auth/login' })}
      >
        <Outlet />
      </AuthProvider>
    </TooltipProvider>
  );
}

/**
 * Pre-hydration theme application. Inline so it runs before the
 * body paints, preventing the flash-of-wrong-theme that would
 * otherwise show light mode for dark-mode users until React hydrates.
 *
 * Reads the `pivox.theme` cookie. If absent (or value is unrecognized),
 * falls back to the legacy `pivox-theme` localStorage entry (which
 * the older client-only implementation wrote) — this keeps existing
 * users' preferences across the migration. The theme-switcher
 * component handles the localStorage → cookie promotion lazily on
 * first read after hydration.
 *
 * Resolves 'system' via prefers-color-scheme so even system-preference
 * users get the right paint on first frame.
 *
 * Wrapped in try/catch because some browsers throw on
 * matchMedia/localStorage access in particular sandboxes; we'd
 * rather paint light-mode than crash the page.
 */
const THEME_INLINE_SCRIPT = `(function(){try{
var c=document.cookie.match(/(?:^|; )pivox\\.theme=([^;]+)/);
var t=c&&c[1];
if(t!=="light"&&t!=="dark"&&t!=="system"){
  var l=localStorage.getItem("pivox-theme");
  t=(l==="light"||l==="dark"||l==="system")?l:"system";
}
var d=t==="system"?window.matchMedia("(prefers-color-scheme:dark)").matches:t==="dark";
if(d)document.documentElement.classList.add("dark");
}catch(e){}})()`;

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" dir="ltr" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INLINE_SCRIPT }} />
        <HeadContent />
      </head>
      <body className="min-h-screen bg-background font-sans text-foreground antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
