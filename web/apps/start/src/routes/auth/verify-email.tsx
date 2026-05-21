import { VerifyEmailFeature } from '@pivox/features/verify-email';
import { asyncHandler } from '@pivox/observability';
import { VerifyEmailCard } from '@pivox/ui/verify-email-card';
import { createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/verify-email')({
  component: VerifyEmailPage,
});

function VerifyEmailPage() {
  const router = useRouter();

  return (
    <VerifyEmailFeature>
      <VerifyEmailCard.Root>
        <VerifyEmailCard.Header />
        <VerifyEmailCard.Message />
        <VerifyEmailCard.ResendButton />
        <VerifyEmailCard.Footer
          onClick={asyncHandler(() => router.navigate({ to: '/auth/login' }))}
        />
      </VerifyEmailCard.Root>
    </VerifyEmailFeature>
  );
}
