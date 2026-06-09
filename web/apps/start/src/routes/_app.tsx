import { organizationId } from '@pivox/client';
import { AppShellFeature } from '@pivox/features/app-shell';
import { useAuth, usePivoxUserId } from '@pivox/features/auth';
import { ChatModalFeature } from '@pivox/features/chat';
import { SidebarInset, SidebarTrigger } from '@pivox/primitives/sidebar';
import { AppShell, useAppShellContext } from '@pivox/ui/app-shell';
import { SidebarProvider } from '@pivox/ui/sidebar-provider';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import {
  Outlet,
  createFileRoute,
  redirect,
  useRouter,
} from '@tanstack/react-router';
import { Suspense, lazy, useCallback } from 'react';

import { $api } from '@/lib/api-client';
import { getServerSession } from '@/server/auth-session';
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

// Lazy-load the profile dialog so it's client-only — it depends on
// AuthContext (Firebase user) which isn't available during SSR.
const ProfileDialog = lazy(() => import('./_app/-profile-dialog'));

export const Route = createFileRoute('/_app')({
  /**
   * Server-side auth gate + SSR prefetch. Runs on both SSR and
   * client-side navigations.
   *
   * Auth: three outcomes per the cookie-state matrix:
   *   1. Valid session     → continue, pass user via route context
   *   2. Invalid cookie    → redirect /auth/verify-session for the
   *      (expired / revoked)  silent-recovery flow (client-side
   *                           Firebase JS likely still has a valid
   *                           refresh token)
   *   3. No cookie at all  → redirect /auth/login (cold visit — no
   *                          recovery to attempt)
   *
   * Prefetch: on the SSR pass only (typeof window === 'undefined'),
   * fetch the caller's orgs via an SA-signed actor JWT and prime
   * the route's QueryClient. The client's useQuery hits the cached
   * entry on hydration — no skeleton flash for the nav picker on
   * cold loads. Client-side navigations skip this; the client's
   * own useQuery handles fetching once mounted.
   */
  beforeLoad: async ({ context, location }) => {
    const { user, cookiePresent } = await getServerSession();
    if (!user) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({
        to: cookiePresent ? '/auth/verify-session' : '/auth/login',
        search: { return: location.pathname + location.searchStr },
        replace: true,
      });
    }

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
    if (typeof window === 'undefined' && user.pivoxUserId) {
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
      initialActiveOrganization,
      initialTheme,
      initialSidebarOpen,
    };
  },
  component: AppLayoutRoute,
});

function AppLayoutRoute() {
  const router = useRouter();
  const { user, initialActiveOrganization, initialTheme, initialSidebarOpen } =
    Route.useRouteContext();
  return (
    <AppShellFeature
      $api={$api}
      // Seed the shell with the server-verified user so the nav-
      // user menu paints with name + photo on first SSR render,
      // not a half-rendered avatar that pops in after Firebase JS
      // resolves on hydration.
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
      <Suspense>
        <ProfileDialog />
      </Suspense>
      <ChatFab />
    </AppShellFeature>
  );
}

/**
 * Floating chat FAB, mounted in the authed shell so chat is reachable
 * on every route (replaces the old standalone /chat route). Builds the
 * chat `parent` + Firebase-token getter the same way that route did.
 * Renders nothing until an org is selected — chat is scoped to an org.
 *
 * Uses the same `usePivoxUserId` hook as the Electron renderer, seeded
 * with the server-verified id from `_app` route context so the FAB
 * server-renders; the hook then re-resolves from the live client claim.
 */
function ChatFab() {
  const { state: shellState } = useAppShellContext();
  const { user: firebaseUser } = useAuth();
  const { user } = Route.useRouteContext();
  const activeOrg = shellState.activeOrganization;
  const pivoxUserId = usePivoxUserId(user.pivoxUserId);

  const getAuthToken = useCallback(async () => {
    if (!firebaseUser) {
      throw new Error('Firebase user not available');
    }
    return firebaseUser.getIdToken();
  }, [firebaseUser]);

  if (!activeOrg || !pivoxUserId) return null;

  const parent = `organizations/${organizationId(activeOrg)}/users/${pivoxUserId}`;
  // key={parent} remounts the runtime when the active org changes. The
  // FAB is mounted shell-wide (persists across navigation), so without
  // this an org switch would keep the previous org's conversation id in
  // the runtime's state and smuggle it into the new org's next turn.
  return (
    <ChatModalFeature
      key={parent}
      parent={parent}
      getAuthToken={getAuthToken}
    />
  );
}
