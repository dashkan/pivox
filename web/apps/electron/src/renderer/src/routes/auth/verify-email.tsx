import { createFileRoute, useRouter } from '@tanstack/react-router';
import { VerifyEmailFeature } from '@pivox/features/verify-email';
import { VerifyEmailCard } from '@pivox/ui/verify-email-card';

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
          onClick={() => router.navigate({ to: '/auth/login' })}
        />
      </VerifyEmailCard.Root>
    </VerifyEmailFeature>
  );
}
