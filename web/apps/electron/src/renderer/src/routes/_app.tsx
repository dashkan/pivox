import { AuthGateFeature } from '@pivox/features/auth-gate';
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from '@pivox/primitives/sidebar';
import { AppSidebar } from '@pivox/ui/app-shell';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { Outlet, createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
});

/**
 * Post-auth app shell. shadcn sidebar-07 layout, wrapped in
 * AuthGateFeature since Electron has no SSR-side auth gate (no
 * server beforeLoad like start) — the client-side gate redirects
 * unauthed users to /auth/login.
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
    <AuthGateFeature>
      <SidebarProvider>
        <AppSidebar />
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
    </AuthGateFeature>
  );
}
