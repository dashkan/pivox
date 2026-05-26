import { Separator } from '@pivox/primitives/separator';
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from '@pivox/primitives/sidebar';
import { AppSidebar } from '@pivox/ui/app-shell';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { Outlet, createFileRoute, redirect } from '@tanstack/react-router';

import { getServerSession } from '@/server/auth-session';

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

/**
 * Post-auth app shell. shadcn sidebar-07 layout: collapsible
 * sidebar on the left, main content area on the right. Top bar
 * inside the inset carries the sidebar trigger + theme toggle
 * (the trigger is the only way to collapse the sidebar when no
 * keyboard shortcut is bound).
 *
 * Stage C of the post-login layout work: the sidebar mounts with
 * SAMPLE DATA from packages/ui/src/app-shell/app-sidebar.tsx. The
 * profile-dialog and sign-out interactions in the nav-user menu
 * are wired to stubs (// wired in Stage B2) — Stage B2 brings in
 * the AppShellFeature that connects orgs/spaces queries +
 * profile-dialog state + useAuth().signOut().
 */
function AppLayoutRoute() {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator
            orientation="vertical"
            className="mr-2 data-[orientation=vertical]:h-4"
          />
          <div className="ms-auto">
            <ThemeSwitcher />
          </div>
        </header>
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  );
}
