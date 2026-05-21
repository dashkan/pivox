import { RegistrationFeature } from '@pivox/features/registration';
import { asyncHandler } from '@pivox/observability';
import { RegistrationCard } from '@pivox/ui/registration-card';
import { authProviders } from '@renderer/lib/auth-providers';
import { createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/register')({
  component: RegisterPage,
});

function RegisterPage() {
  const router = useRouter();

  return (
    <RegistrationFeature
      onSuccess={asyncHandler(() =>
        router.navigate({ to: '/auth/verify-email' }),
      )}
      onLinkRequired={asyncHandler(() =>
        router.navigate({ to: '/auth/link-account' }),
      )}
    >
      <RegistrationCard.Root>
        <RegistrationCard.Header />
        <RegistrationCard.EmailField />
        <RegistrationCard.DisplayNameField />
        <RegistrationCard.PasswordField />
        <RegistrationCard.ConfirmPasswordField />
        <RegistrationCard.SubmitButton />
        <RegistrationCard.Separator />
        <RegistrationCard.SocialButtons providers={authProviders} />
        <RegistrationCard.Footer
          onClick={asyncHandler(() => router.navigate({ to: '/auth/login' }))}
        />
      </RegistrationCard.Root>
    </RegistrationFeature>
  );
}
