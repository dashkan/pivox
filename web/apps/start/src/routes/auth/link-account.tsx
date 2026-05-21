import { LinkAccountFeature } from '@pivox/features/link-account';
import { asyncHandler } from '@pivox/observability';
import { LinkAccountCard } from '@pivox/ui/link-account-card';
import { createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/link-account')({
  component: LinkAccountPage,
});

function LinkAccountPage() {
  const router = useRouter();

  return (
    <LinkAccountFeature onSuccess={asyncHandler(() => router.navigate({ to: '/' }))}>
      <LinkAccountCard.Root>
        <LinkAccountCard.Header />
        <LinkAccountCard.PasswordField />
        <LinkAccountCard.SubmitButton />
        <LinkAccountCard.Footer
          onClick={asyncHandler(() => router.navigate({ to: '/auth/login' }))}
        />
      </LinkAccountCard.Root>
    </LinkAccountFeature>
  );
}
