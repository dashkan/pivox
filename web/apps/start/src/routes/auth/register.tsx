import { RegistrationFeature } from '@pivox/features/registration';
import { asyncHandler } from '@pivox/observability';
import { RegistrationCard } from '@pivox/ui/registration-card';
import { createFileRoute, useRouter } from '@tanstack/react-router';

import { authProviders } from '@/lib/auth-providers';
import { browserRedirectTransport } from '@/lib/browser-redirect-transport';

export const Route = createFileRoute('/auth/register')({
  component: RegisterPage,
});

function RegisterPage() {
  const router = useRouter();

  return (
    <RegistrationFeature
      transport={browserRedirectTransport}
      onSuccess={asyncHandler(() => router.navigate({ to: '/' }))}
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
