import { AppLayoutFeature } from '@pivox/features/app-layout';
import { AuthGateFeature } from '@pivox/features/auth-gate';
import { AppLayout, useAppLayoutContext } from '@pivox/ui/app-layout';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { ElectronUserProfileFeature } from '@renderer/components/electron-user-profile-feature';
import { authProviders } from '@renderer/lib/auth-providers';
import { Outlet, createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
});

function AppLayoutRoute() {
  return (
    <AuthGateFeature>
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
        <ProfileDialog />
      </AppLayoutFeature>
    </AuthGateFeature>
  );
}

function ProfileDialog() {
  const { state, actions } = useAppLayoutContext();

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
