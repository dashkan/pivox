import { AppShellFeature } from '@pivox/features/app-shell';
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from '@pivox/primitives/sidebar';
import { AppShell } from '@pivox/ui/app-shell';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import {
  Outlet,
  createFileRoute,
  redirect,
  useRouter,
} from '@tanstack/react-router';
import { Suspense, lazy } from 'react';

import { $api } from '@/lib/api-client';
import { getServerSession } from '@/server/auth-session';

// Lazy-load the profile dialog so it's client-only — it depends on
// AuthContext (Firebase user) which isn't available during SSR.
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
   */
  beforeLoad: async ({ location }) => {
    const { user, cookiePresent } = await getServerSession();
    if (user) return { user };
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({
      to: cookiePresent ? '/auth/verify-session' : '/auth/login',
      search: { return: location.pathname + location.searchStr },
      replace: true,
    });
  },
  component: AppLayoutRoute,
});

function AppLayoutRoute() {
  const router = useRouter();
  return (
    <AppShellFeature
      $api={$api}
      onCreateOrganization={() => {
        void router.navigate({ to: '/auth/create-org' });
      }}
    >
      <SidebarProvider>
        <AppShell.Sidebar />
        <SidebarInset>
          <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            <div className="ms-auto">
              <ThemeSwitcher />
            </div>
          </header>
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
      <Suspense>
        <ProfileDialog />
      </Suspense>
    </AppShellFeature>
  );
}
