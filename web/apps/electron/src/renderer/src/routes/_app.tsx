import { AppShellFeature } from '@pivox/features/app-shell';
import { AuthGateFeature } from '@pivox/features/auth-gate';
import { SidebarInset, SidebarTrigger } from '@pivox/primitives/sidebar';
import { AppShell, useAppShellContext } from '@pivox/ui/app-shell';
import { SidebarProvider } from '@pivox/ui/sidebar-provider';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { ElectronUserProfileFeature } from '@renderer/components/electron-user-profile-feature';
import { $api } from '@renderer/lib/api-client';
import { authProviders } from '@renderer/lib/auth-providers';
import { Outlet, createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
});

/**
 * Post-auth app shell. shadcn sidebar-07 layout wrapped in
 * AuthGateFeature since Electron has no SSR-side auth gate — the
 * client-side gate redirects unauthed users to /auth/login.
 */
function AppLayoutRoute() {
  const router = useRouter();
  return (
    <AuthGateFeature>
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
        <ProfileDialog />
      </AppShellFeature>
    </AuthGateFeature>
  );
}

/**
 * Electron-specific profile dialog. Wraps the UserProfileCard
 * primitives in ElectronUserProfileFeature (which provides the
 * Electron-specific provider-link UX). Consumes AppShellContext
 * for the open state + setter — wired in by the AppShell.NavUser's
 * "Manage Account" menu item via actions.setProfileOpen(true).
 */
function ProfileDialog() {
  const { state, actions } = useAppShellContext();

  return (
    <ElectronUserProfileFeature
      onClose={() => {
        actions.setProfileOpen(false);
      }}
      open={state.profileOpen}
      providers={authProviders}
    >
      <UserProfileCard.Root
        open={state.profileOpen}
        onOpenChange={actions.setProfileOpen}
      >
        <UserProfileCard.Sidebar />
        <UserProfileCard.AccountPage />
        <UserProfileCard.SecurityPage />
      </UserProfileCard.Root>
    </ElectronUserProfileFeature>
  );
}
