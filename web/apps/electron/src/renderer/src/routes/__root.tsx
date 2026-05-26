import { AuthProvider } from '@pivox/features/auth';
import { TooltipProvider } from '@pivox/primitives/tooltip';
import { Outlet, createRootRoute } from '@tanstack/react-router';

export const Route = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  return (
    // TooltipProvider at the root so Radix Tooltip consumers
    // (currently SidebarMenuButton's tooltip prop in @pivox/ui/
    // app-shell, anywhere else later) find an ancestor context.
    // delayDuration={0} matches shadcn's recommended sidebar shape —
    // the icon-collapsed sidebar relies on instant tooltips to be
    // navigable; the default 700ms feels broken.
    <TooltipProvider delayDuration={0}>
      <AuthProvider>
        <div className="min-h-screen bg-background font-sans text-foreground antialiased">
          <Outlet />
        </div>
      </AuthProvider>
    </TooltipProvider>
  );
}
