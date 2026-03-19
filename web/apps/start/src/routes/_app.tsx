import { lazy, Suspense } from 'react'
import { Outlet, createFileRoute, useRouter } from '@tanstack/react-router'
import { AppLayoutFeature } from '@pivox/features/app-layout'
import { AppLayout } from '@pivox/ui/app-layout'
import { ThemeSwitcher } from '@pivox/ui/theme-switcher'

// Lazy-load the profile dialog so it's client-only — it depends on
// auth context which isn't available during SSR.
const ProfileDialog = lazy(() => import('./_app/-profile-dialog'))

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
      <Suspense>
        <ProfileDialog />
      </Suspense>
    </AppLayoutFeature>
  )
}
