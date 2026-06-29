import { CreateOrgFeature } from '@pivox/features/create-org';
import { asyncHandler } from '@pivox/observability';
import { CreateOrgCard } from '@pivox/ui/create-org-card';
import { createFileRoute, useRouter } from '@tanstack/react-router';

import { apiClient } from '@/lib/api-client';
import { requireKcSession } from '@/lib/auth-gate';
import { KeycloakAuthProvider } from '@/lib/kc-auth-provider';

/**
 * Post-login org-creation flow. It sits OUTSIDE `_app` deliberately: the user
 * here has no organization yet, so rendering the full app shell (org picker,
 * spaces nav) makes no sense — this is a standalone card. But `CreateOrgFeature`
 * calls `useAuth().signOut`, so it still needs an AuthContext. We run the same
 * `requireKcSession` gate as `_app` (redirecting to sign-in if there's no
 * session) and wrap the card in `KeycloakAuthProvider` with the resolved user.
 */
export const Route = createFileRoute('/auth/create-org')({
  beforeLoad: async ({ location }) => {
    const { user } = await requireKcSession(location);
    return { user };
  },
  component: CreateOrgPage,
});

function CreateOrgPage() {
  const router = useRouter();
  const { user } = Route.useRouteContext();
  return (
    <KeycloakAuthProvider user={user}>
      <CreateOrgFeature
        apiClient={apiClient}
        onSuccess={asyncHandler(() => router.navigate({ to: '/' }))}
      >
        <CreateOrgCard.Root>
          <CreateOrgCard.Header />
          <CreateOrgCard.DisplayNameField />
          <CreateOrgCard.ShortNameField />
          <CreateOrgCard.SlugHint />
          <CreateOrgCard.SubmitButton />
          <CreateOrgCard.Footer />
        </CreateOrgCard.Root>
      </CreateOrgFeature>
    </KeycloakAuthProvider>
  );
}
