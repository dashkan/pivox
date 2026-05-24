import { AppLayoutFeature } from '@pivox/features/app-layout';
import { OrgGateFeature } from '@pivox/features/org-gate';
import { VerifyEmailLink } from '@pivox/features/verify-email';
import { asyncHandler } from '@pivox/observability';
import { AppLayout } from '@pivox/ui/app-layout';
import { ThemeSwitcher } from '@pivox/ui/theme-switcher';
import { Outlet, createFileRoute, useRouter } from '@tanstack/react-router';
import { Suspense, lazy } from 'react';

import { apiClient } from '@/lib/api-client';

// Lazy-load the profile dialog so it's client-only — it depends on
// auth context which isn't available during SSR.
const ProfileDialog = lazy(() => import('./_app/-profile-dialog'));

export const Route = createFileRoute('/_app')({
  component: AppLayoutRoute,
});

function AppLayoutRoute() {
  const router = useRouter();

  return (
    <OrgGateFeature
      apiClient={apiClient}
      onCreateOrgRequired={() => {
        void router.navigate({ to: '/auth/create-org' });
      }}
    >
      <AppLayoutFeature
        onNavigateToLogin={asyncHandler(() =>
          router.navigate({ to: '/auth/login' }),
        )}
      >
        <AppLayout.Root>
          <AppLayout.Header>
            <AppLayout.HeaderTitle>Pivox</AppLayout.HeaderTitle>
            <AppLayout.HeaderNav>
              <VerifyEmailLink
                onClick={() => {
                  void router.navigate({ to: '/auth/verify-email' });
                }}
              />
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
    </OrgGateFeature>
  );
}
