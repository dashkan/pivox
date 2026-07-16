import { organizationId } from '@pivox/client';
import { AppShellFeature } from '@pivox/features/app-shell';
import { useUserId } from '@pivox/features/auth';
import { ChatModalFeature } from '@pivox/features/chat';
import { SidebarInset, SidebarTrigger } from '@pivox/primitives/sidebar';
import { AppShell, useAppShellContext } from '@pivox/ui/app-shell';
import { SidebarProvider } from '@pivox/ui/sidebar-provider';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import {
  Outlet,
  createFileRoute,
  useRouter,
  useRouterState,
} from '@tanstack/react-router';
import { ShieldIcon, WorkflowIcon } from 'lucide-react';

import type { NavMainItem } from '@pivox/ui/app-shell';

import { $api } from '@/lib/api-client';
import { requireKcSession } from '@/lib/auth-gate';
import { KeycloakAuthProvider } from '@/lib/kc-auth-provider';
import {
  getActiveOrgCookie,
  prefetchOrgsForCurrentUser,
  prefetchSpacesForActiveOrg,
} from '@/server/prefetch';
import {
  getSidebarOpenCookie,
  getThemeCookie,
  type Theme,
} from '@/server/prefs';

export const Route = createFileRoute('/_app')({
  /**
   * Server-side auth gate + SSR prefetch. Runs on both SSR and
   * client-side navigations.
   *
   * Auth: `requireKcSession` reads the Keycloak BFF session. No
   * session → full-page navigation to the `/auth/sign-in` server
   * handler (SSR 302 / client `window.location`), preserving the
   * return path. Otherwise the resolved user + account-console URL
   * flow into route context.
   *
   * Prefetch: on the SSR pass only (typeof window === 'undefined'),
   * fetch the caller's orgs with the user's Keycloak access token
   * (from the session cookie) and prime the route's QueryClient. The
   * client's useQuery hits the cached entry on hydration — no
   * skeleton flash for the nav picker on cold loads. Client-side
   * navigations skip this; the client's own useQuery handles
   * fetching once mounted.
   */
  beforeLoad: async ({ context, location }) => {
    const { user, accountConsoleUrl } = await requireKcSession(location);

    // SSR-only prefetch. typeof window is the standard guard for
    // server-pass detection in TanStack Start. On the client side,
    // the prefetch server-fns would be HTTP RPC roundtrips —
    // wasteful when the client's useQuery is about to fetch the
    // same data directly. Orgs + spaces fire concurrently because
    // they're independent; spaces depends on the active-org cookie
    // (written by the SPA) rather than on orgs query data.
    //
    // initialActiveOrganization carries the cookie value through to
    // the shell so the client's useAppShell can use it as a
    // synchronous lazy-state initializer. Without this, the client
    // would mount with state=null and the validation effect would
    // race the cookie-read effect — silently overwriting the user's
    // selection with orgs[0] on every refresh.
    let initialActiveOrganization: string | null = null;
    let initialTheme: Theme | null = null;
    let initialSidebarOpen: boolean | null = null;
    if (typeof window === 'undefined') {
      // Fire the cookie reads alongside the prefetches — same request,
      // server-fn dispatch is in-process so this is effectively a
      // single batch with no extra round-trips.
      const [activeOrgCookie, themeCookie, sidebarOpenCookie, orgs, spaces] =
        await Promise.all([
          getActiveOrgCookie(),
          getThemeCookie(),
          getSidebarOpenCookie(),
          prefetchOrgsForCurrentUser(),
          prefetchSpacesForActiveOrg(),
        ]);
      initialActiveOrganization = activeOrgCookie;
      initialTheme = themeCookie;
      initialSidebarOpen = sidebarOpenCookie;
      if (orgs) {
        // queryKey from openapi-react-query is deterministic on
        // (method, path, params) — server-built queryOptions
        // produces the same key the client's useQuery uses, so
        // setQueryData primes the entry the client will read.
        const { queryKey } = $api.queryOptions(
          'get',
          '/v1/accounts/me/organizations',
          { params: { path: { parent: 'accounts/me' } } },
        );
        context.queryClient.setQueryData(queryKey, orgs);
      }
      if (spaces) {
        // Match the client-side query shape in useAppShell:
        // path param is the org SLUG, not the full resource name.
        const { queryKey } = $api.queryOptions(
          'get',
          '/v1/organizations/{organization}/spaces',
          { params: { path: { organization: spaces.orgSlug } } },
        );
        context.queryClient.setQueryData(queryKey, spaces.spaces);
      }
    }

    return {
      user,
      accountConsoleUrl,
      initialActiveOrganization,
      initialTheme,
      initialSidebarOpen,
    };
  },
  component: AppLayoutRoute,
});

/**
 * Sidebar nav groups. `href` on a group is unused (the trigger toggles the
 * group); subitems carry the real routes. `isActive` opens the group holding
 * the current route. "Definitions" points at the workflows catalog (T5);
 * "Connectors" and "Secrets" ship here.
 */
function useNavMain(): NavMainItem[] {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return [
    {
      title: 'Workflows',
      href: '/workflows',
      icon: <WorkflowIcon />,
      isActive:
        pathname.startsWith('/workflows') || pathname.startsWith('/connectors'),
      items: [
        { title: 'Definitions', href: '/workflows' },
        { title: 'Connectors', href: '/connectors' },
      ],
    },
    {
      title: 'Admin',
      href: '/secrets',
      icon: <ShieldIcon />,
      isActive: pathname.startsWith('/secrets'),
      items: [{ title: 'Secrets', href: '/secrets' }],
    },
  ];
}

function AppLayoutRoute() {
  const router = useRouter();
  const navMain = useNavMain();
  const {
    user,
    accountConsoleUrl,
    initialActiveOrganization,
    initialTheme,
    initialSidebarOpen,
  } = Route.useRouteContext();
  return (
    <KeycloakAuthProvider user={user}>
      <AppShellFeature
        $api={$api}
        navMain={navMain}
        // Seed the shell with the server-verified user so the nav-
        // user menu paints with name + photo on first SSR render,
        // not a half-rendered avatar that pops in after the client
        // resolves auth.
        initialUser={{
          displayName: user.displayName,
          email: user.email,
          photoURL: user.photoURL,
        }}
        // SSR-resolved active org from the cookie. Without this, the
        // hook's lazy-state init would see `null` during SSR (no
        // document.cookie on the server), producing an HTML payload
        // that doesn't match the client's first paint.
        initialActiveOrganization={initialActiveOrganization}
        onCreateOrganization={() => {
          void router.navigate({ to: '/auth/create-org' });
        }}
        // "Manage Account" opens the Keycloak account console in a new
        // tab (the BFF has no in-app profile UI — account management
        // lives in Keycloak). `undefined` when the issuer isn't
        // configured; nav-user then falls back to its no-op default.
        onOpenAccount={
          accountConsoleUrl
            ? () => {
                window.open(accountConsoleUrl, '_blank', 'noopener');
              }
            : undefined
        }
      >
        {/* SSR-resolved sidebar open state from the cookie. Without
            this, the wrapper's useState lazy initializer would see
            `null` during SSR and use the default `true`, producing
            HTML that doesn't match the client's first paint when the
            user had previously collapsed the sidebar. */}
        <SidebarProvider initialOpen={initialSidebarOpen ?? undefined}>
          <AppShell.Sidebar />
          <SidebarInset>
            <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
              <SidebarTrigger className="-ml-1" />
              <div className="ms-auto">
                {/* SSR-resolved theme from the cookie. Without this,
                    useSyncExternalStore's server snapshot would return
                    the 'system' default and the icon would flicker to
                    the user's actual saved theme on hydration. */}
                <ThemeSwitcher initialTheme={initialTheme ?? undefined} />
              </div>
            </header>
            <Outlet />
          </SidebarInset>
        </SidebarProvider>
        <ChatFab />
      </AppShellFeature>
    </KeycloakAuthProvider>
  );
}

/**
 * Floating chat FAB, mounted in the authed shell so chat is reachable
 * on every route (replaces the old standalone /chat route). Renders
 * nothing until an org is selected — chat is scoped to an org.
 *
 * Under the BFF the browser holds no bearer, so chat goes through the
 * same-origin `/api` proxy (`baseUrl="/api"`), which injects the
 * Keycloak access token from the httpOnly cookie. `useUserId()`
 * reads the id from the KeycloakAuthProvider (== KC `sub`).
 */
function ChatFab() {
  const { state: shellState } = useAppShellContext();
  const activeOrg = shellState.activeOrganization;
  const userId = useUserId();

  if (!activeOrg || !userId) return null;

  const parent = `organizations/${organizationId(activeOrg)}/users/${userId}`;
  // key={parent} remounts the runtime when the active org changes. The
  // FAB is mounted shell-wide (persists across navigation), so without
  // this an org switch would keep the previous org's conversation id in
  // the runtime's state and smuggle it into the new org's next turn.
  return <ChatModalFeature key={parent} parent={parent} baseUrl="/api" />;
}
