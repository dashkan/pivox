import { organizationId, spaceId } from '@pivox/client';
import { AppShellFeature } from '@pivox/features/app-shell';
import { useUserId } from '@pivox/features/auth';
import { ChatModalFeature } from '@pivox/features/chat';
import { SidebarInset, SidebarTrigger } from '@pivox/primitives/sidebar';
import { ACTIVE_ORG, storage } from '@pivox/storage';
import { AppShell, useAppShellContext } from '@pivox/ui/app-shell';
import { SidebarProvider } from '@pivox/ui/sidebar-provider';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import {
  Outlet,
  createFileRoute,
  useParams,
  useRouter,
  useRouterState,
} from '@tanstack/react-router';
import { useCallback } from 'react';

import { $api } from '@/lib/api-client';
import { requireKcSession } from '@/lib/auth-gate';
import { KeycloakAuthProvider } from '@/lib/kc-auth-provider';
import { orgNavTarget, spaceNavTarget } from '@/lib/scope-nav';
import { useNavMain } from '@/lib/use-nav-main';
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

function AppLayoutRoute() {
  const router = useRouter();
  const {
    user,
    accountConsoleUrl,
    initialActiveOrganization,
    initialTheme,
    initialSidebarOpen,
  } = Route.useRouteContext();

  // The URL is the single source of truth for scope. Read the current route's
  // `$organization` / `$space` slugs (present under the scoped tree) and derive
  // the shell's active scope from them. Routes without an org param (the org
  // selector, and Electron with no SSR) leave the shell UNCONTROLLED, falling
  // back to its cookie-driven state.
  const params = useParams({ strict: false }) as {
    organization?: string;
    space?: string;
  };
  const orgSlugParam = params.organization;
  const spaceSlugParam = params.space;

  // Space-select keeps the user on the current resource's space variant, so the
  // handler reads the resource off the pathname.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const controlledActiveOrganization = orgSlugParam
    ? `organizations/${orgSlugParam}`
    : undefined;
  const controlledActiveSpace = orgSlugParam
    ? spaceSlugParam
      ? `organizations/${orgSlugParam}/spaces/${spaceSlugParam}`
      : null
    : undefined;

  // Nav derives the Connectors link's org from the URL param or the live
  // last-visited cookie (see use-nav-main). Kept a hook so it's unit-testable.
  const navMain = useNavMain(initialActiveOrganization);

  // Org select: keep the user on the current section in the new org (org-rollup
  // scope — a space can't carry across orgs), and write the last-visited hint.
  // The URL then owns scope; the cookie is only a hint for the next cold `/` visit.
  const onSelectOrganization = useCallback(
    (organization: string) => {
      storage.set(ACTIVE_ORG, organization);
      router.history.push(orgNavTarget(pathname, organizationId(organization)));
    },
    [router, pathname],
  );

  // Space select: narrow (or roll up) within the current org, keeping the user on
  // the CURRENT resource's space variant (derived from the pathname by the pure
  // `spaceNavTarget` — connectors→space connectors, secrets→space secrets). A
  // workflows context (org-direct) falls back to that org's connectors, per
  // `spaceNavTarget`, though the picker's Spaces section is suppressed there so
  // this is only a defensive path. A no-op off the scoped tree (no org context).
  const onSelectSpace = useCallback(
    (space: string | null) => {
      if (!orgSlugParam) return;
      router.history.push(
        spaceNavTarget(pathname, orgSlugParam, space ? spaceId(space) : null),
      );
    },
    [router, orgSlugParam, pathname],
  );

  return (
    <KeycloakAuthProvider user={user}>
      <AppShellFeature
        $api={$api}
        navMain={navMain}
        activeOrganization={controlledActiveOrganization}
        activeSpace={controlledActiveSpace}
        onSelectOrganization={onSelectOrganization}
        onSelectSpace={onSelectSpace}
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
