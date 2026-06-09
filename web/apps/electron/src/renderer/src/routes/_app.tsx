import { organizationId } from '@pivox/client';
import { AppShellFeature } from '@pivox/features/app-shell';
import { useAuth, usePivoxUserId } from '@pivox/features/auth';
import { AuthGateFeature } from '@pivox/features/auth-gate';
import { ChatModalFeature } from '@pivox/features/chat';
import { SidebarInset, SidebarTrigger } from '@pivox/primitives/sidebar';
import { AppShell, useAppShellContext } from '@pivox/ui/app-shell';
import { SidebarProvider } from '@pivox/ui/sidebar-provider';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { ElectronUserProfileFeature } from '@renderer/components/electron-user-profile-feature';
import { $api } from '@renderer/lib/api-client';
import { authProviders } from '@renderer/lib/auth-providers';
import { Outlet, createFileRoute, useRouter } from '@tanstack/react-router';
import { useCallback } from 'react';

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
        <ChatFab />
      </AppShellFeature>
    </AuthGateFeature>
  );
}

// The Electron renderer is cross-origin to the API (file:// in packaged
// builds, http://localhost:5173 in dev), so chat requests target a
// configured remote via VITE_BASE_URL — same as the rest of the
// renderer's transport plumbing.
const CHAT_BASE_URL =
  import.meta.env.VITE_BASE_URL ?? 'https://pivox.ngrok.app';

/**
 * Floating chat FAB, mounted in the authed shell so chat is reachable
 * on every route (replaces the old standalone /chat route). Sources the
 * Pivox user UUID from the client Firebase ID-token claim
 * (`usePivoxUserId`) — Electron has no server session. Renders nothing
 * until an org is selected and the claim has resolved.
 */
function ChatFab() {
  const { state: shellState } = useAppShellContext();
  const { user: firebaseUser } = useAuth();
  const pivoxUserId = usePivoxUserId();
  const activeOrg = shellState.activeOrganization;

  const getAuthToken = useCallback(async () => {
    if (!firebaseUser) {
      throw new Error('Firebase user not available');
    }
    return firebaseUser.getIdToken();
  }, [firebaseUser]);

  if (!activeOrg || !pivoxUserId) return null;

  const parent = `organizations/${organizationId(activeOrg)}/users/${pivoxUserId}`;
  // key={parent} remounts the runtime on org switch so the shell-wide
  // mount can't carry an old org's conversation id into a new org's
  // turn (see the start renderer for the full rationale).
  return (
    <ChatModalFeature
      key={parent}
      parent={parent}
      baseUrl={CHAT_BASE_URL}
      getAuthToken={getAuthToken}
    />
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
