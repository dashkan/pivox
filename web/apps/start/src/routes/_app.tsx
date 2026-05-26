import { AppLayoutFeature } from '@pivox/features/app-layout';
import { OrgGateFeature } from '@pivox/features/org-gate';
import { AppLayout } from '@pivox/ui/app-layout';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { Outlet, createFileRoute, redirect } from '@tanstack/react-router';
import { Suspense, lazy } from 'react';

import { apiClient } from '@/lib/api-client';
import { getServerSession } from '@/server/auth-session';

// Lazy-load the profile dialog so it's client-only — it depends on
// auth context which isn't available during SSR.
const ProfileDialog = lazy(() => import('./_app/-profile-dialog'));

export const Route = createFileRoute('/_app')({
  /**
   * Server-side auth gate. Runs on both SSR and client-side
   * navigations (the latter does an HTTP round-trip to invoke the
   * server function). Three outcomes per the cookie-state matrix:
   *
   *   1. Valid session     → continue, pass user via route context
   *   2. Invalid cookie    → redirect /auth/verify-session for the
   *      (expired / revoked)  silent-recovery flow (client-side
   *                           Firebase JS likely still has a valid
   *                           refresh token)
   *   3. No cookie at all  → redirect /auth/login (cold visit — no
   *                          recovery to attempt)
   *
   * The `?return=<path>` search param carries the originally-requested
   * URL so the recovery / login flow can land the user back where
   * they tried to go.
   *
   * AuthGateFeature was removed from the subtree — with the
   * server-side gate handling redirects before render, the
   * client-side gate would only add a second loading splash for the
   * brief moment between Firebase JS init and the route's first
   * paint. Sign-out flows through `clearSession` (cookie cleared)
   * then Firebase signOut, and the next navigation re-runs this
   * beforeLoad against a now-empty cookie → user lands on login.
   */
  beforeLoad: async ({ location }) => {
    const { user, cookiePresent } = await getServerSession();
    if (user) return { user };
    // TanStack Router's `redirect()` returns a special Redirect
    // sentinel that's MEANT to be thrown — the router catches it and
    // turns it into a navigation. eslint's only-throw-error rule
    // can't see that.
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({
      to: cookiePresent ? '/auth/verify-session' : '/auth/login',
      // `location.href` is the absolute URL (https://app.../foo); the
      // return-URL validators on /auth/login + /auth/verify-session
      // require a path-relative string (must start with a single
      // slash) to prevent open redirects. Send pathname+searchStr so
      // the destination is preserved cleanly through the auth flow.
      search: { return: location.pathname + location.searchStr },
      replace: true,
    });
  },
  component: AppLayoutRoute,
});

function AppLayoutRoute() {
  return (
    <OrgGateFeature apiClient={apiClient}>
      <AppLayoutFeature>
        <AppLayout.Root>
          <AppLayout.Header>
            <AppLayout.HeaderTitle>Pivox</AppLayout.HeaderTitle>
            <AppLayout.HeaderNav>
              <ThemeSwitcher />
              <AppLayout.HeaderAvatar />
            </AppLayout.HeaderNav>
          </AppLayout.Header>
          <AppLayout.Content>
            <Outlet />
          </AppLayout.Content>
        </AppLayout.Root>
        <Suspense>
          <ProfileDialog />
        </Suspense>
      </AppLayoutFeature>
    </OrgGateFeature>
  );
}
