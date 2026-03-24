import { createFileRoute, useRouter } from '@tanstack/react-router';
import { ResetPasswordFeature } from '@pivox/features/reset-password';
import { ResetPasswordCard } from '@pivox/ui/reset-password-card';

type ResetPasswordSearch = {
  oobCode: string;
};

export const Route = createFileRoute('/auth/reset-password')({
  validateSearch: (search: Record<string, unknown>): ResetPasswordSearch => ({
    oobCode: (search.oobCode as string) || '',
  }),
  component: ResetPasswordPage,
});

function ResetPasswordPage() {
  const router = useRouter();
  const { oobCode } = Route.useSearch();

  return (
    <ResetPasswordFeature
      oobCode={oobCode}
      onSuccess={() => router.navigate({ to: '/auth/login' })}
    >
      <ResetPasswordCard.Root>
        <ResetPasswordCard.Header />
        <ResetPasswordCard.PasswordField />
        <ResetPasswordCard.ConfirmPasswordField />
        <ResetPasswordCard.SuccessMessage />
        <ResetPasswordCard.SubmitButton />
        <ResetPasswordCard.Footer
          onClick={() => router.navigate({ to: '/auth/login' })}
        />
      </ResetPasswordCard.Root>
    </ResetPasswordFeature>
  );
}
