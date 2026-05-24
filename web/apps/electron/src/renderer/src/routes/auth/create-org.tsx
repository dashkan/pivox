import { CreateOrgFeature } from '@pivox/features/create-org';
import { asyncHandler } from '@pivox/observability';
import { CreateOrgCard } from '@pivox/ui/create-org-card';
import { apiClient } from '@renderer/lib/api-client';
import { createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/create-org')({
  component: CreateOrgPage,
});

function CreateOrgPage() {
  const router = useRouter();
  return (
    <CreateOrgFeature
      apiClient={apiClient}
      onSuccess={asyncHandler(() => router.navigate({ to: '/' }))}
      onSignOut={asyncHandler(() => router.navigate({ to: '/auth/login' }))}
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
  );
}
