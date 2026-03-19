import { Outlet, createFileRoute, useRouter } from '@tanstack/react-router'
import { AppLayoutFeature } from '@pivox/features/app-layout'
import { ElectronUserProfileFeature } from '../components/electron-user-profile-feature'
import { AppLayout, useAppLayoutContext } from '@pivox/ui/app-layout'
import { UserProfileCard } from '@pivox/ui/user-profile-card'
import { ThemeSwitcher } from '@pivox/ui/theme-switcher'

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
})

function AppLayoutRoute() {
  const router = useRouter()

  return (
    <AppLayoutFeature
      onNavigateToLogin={() => router.navigate({ to: '/auth/login' })}
    >
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
  )
}

function ProfileDialog() {
  const { state, actions } = useAppLayoutContext()

  return (
    <ElectronUserProfileFeature onClose={() => actions.setProfileOpen(false)} open={state.profileOpen}>
      <UserProfileCard.Root
        open={state.profileOpen}
        onOpenChange={actions.setProfileOpen}
      >
        <UserProfileCard.Sidebar />
        <UserProfileCard.AccountPage />
        <UserProfileCard.SecurityPage />
      </UserProfileCard.Root>
    </ElectronUserProfileFeature>
  )
}
