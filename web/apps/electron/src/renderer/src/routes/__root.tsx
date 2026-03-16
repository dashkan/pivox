import { Outlet, createRootRoute } from '@tanstack/react-router'
import { AuthProvider } from '@pivox/features/auth'

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  return (
    <AuthProvider>
      <div className="min-h-screen bg-background font-sans text-foreground antialiased">
        <Outlet />
      </div>
    </AuthProvider>
  )
}
