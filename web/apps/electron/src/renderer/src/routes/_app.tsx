import { AuthGateFeature } from '@pivox/features/auth-gate';
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from '@pivox/primitives/sidebar';
import { AppShell } from '@pivox/ui/app-shell';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { Outlet, createFileRoute } from '@tanstack/react-router';
import { TerminalSquareIcon } from 'lucide-react';

import type { AppShellContextValue } from '@pivox/ui/app-shell';

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
});

/**
 * TEMPORARY sample data for AppShell. Replaced by AppShellFeature in
 * Stage B2 — the feature owns real queries (orgs, spaces), user info
 * from useAuth, active-org persistence in localStorage, and the
 * createOrganization / signOut handlers. `git grep SAMPLE_APP_SHELL`
 * finds the two scaffolded places (start + electron).
 */
const SAMPLE_APP_SHELL: AppShellContextValue = {
  state: {
    user: {
      displayName: 'Sample User',
      email: '[email protected]',
      photoURL: null,
    },
    orgs: [
      { organization: 'organizations/acme', displayName: 'Acme Inc' },
      { organization: 'organizations/example', displayName: 'Example Co' },
    ],
    orgsLoading: false,
    activeOrganization: 'organizations/acme',
    spaces: [],
    spacesLoading: false,
    navMain: [
      {
        title: 'Playground',
        href: '/',
        icon: <TerminalSquareIcon />,
        isActive: true,
        items: [
          { title: 'History', href: '/' },
          { title: 'Starred', href: '/' },
          { title: 'Settings', href: '/' },
        ],
      },
    ],
    profileOpen: false,
  },
  actions: {
    setActiveOrganization: () => undefined,
    createOrganization: () => undefined,
    setProfileOpen: () => undefined,
    signOut: () => undefined,
  },
};

/**
 * Post-auth app shell. shadcn sidebar-07 layout wrapped in
 * AuthGateFeature since Electron has no SSR-side auth gate — the
 * client-side gate redirects unauthed users to /auth/login.
 */
function AppLayoutRoute() {
  return (
    <AuthGateFeature>
      <AppShell.Provider value={SAMPLE_APP_SHELL}>
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
      </AppShell.Provider>
    </AuthGateFeature>
  );
}
