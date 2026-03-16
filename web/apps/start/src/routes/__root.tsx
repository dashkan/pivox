import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRoute,
  useRouter,
} from '@tanstack/react-router'
import { AuthProvider } from '@pivox/features/auth'
import { AppLayoutFeature } from '@pivox/features/app-layout'
import { UserProfileFeature } from '@pivox/features/user-profile'
import { AppLayout, useAppLayoutContext  } from '@pivox/ui/app-layout'
import { UserProfileCard } from '@pivox/ui/user-profile-card'

import appCss from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Pivox' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  component: RootComponent,
  shellComponent: RootDocument,
})

function RootComponent() {
  const router = useRouter()

  return (
    <AuthProvider>
      <AppLayoutFeature
        onNavigateToLogin={() => router.navigate({ to: '/auth/login' })}
      >
        <AppLayout.Root>
          <AppLayout.Header>
            <AppLayout.HeaderTitle>Pivox</AppLayout.HeaderTitle>
            <AppLayout.HeaderNav>
              <AppLayout.HeaderAvatar />
            </AppLayout.HeaderNav>
          </AppLayout.Header>
          <AppLayout.Content>
            <Outlet />
          </AppLayout.Content>
        </AppLayout.Root>
        <ProfileDialog />
      </AppLayoutFeature>
    </AuthProvider>
  )
}

function ProfileDialog() {
  const { state, actions } = useAppLayoutContext()

  return (
    <UserProfileFeature>
      <UserProfileCard.Root
        open={state.profileOpen}
        onOpenChange={actions.setProfileOpen}
      >
        <UserProfileCard.Sidebar />
        <UserProfileCard.AccountPage />
        <UserProfileCard.SecurityPage />
      </UserProfileCard.Root>
    </UserProfileFeature>
  )
}

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" dir="ltr">
      <head>
        <HeadContent />
      </head>
      <body className="min-h-screen bg-background font-sans text-foreground antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  )
}
