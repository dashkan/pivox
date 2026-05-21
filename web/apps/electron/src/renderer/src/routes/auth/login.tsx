import { asyncHandler } from '@pivox/observability';
import { LoginCard } from '@pivox/ui/login-card';
import { ElectronLoginFeature } from '@renderer/components/electron-login-feature';
import { authProviders } from '@renderer/lib/auth-providers';
import { createFileRoute, useRouter } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/login')({ component: LoginPage });

function LoginPage() {
  const router = useRouter();

  return (
    <ElectronLoginFeature
      onSuccess={asyncHandler(() => router.navigate({ to: '/' }))}
      onLinkRequired={asyncHandler(() =>
        router.navigate({ to: '/auth/link-account' }),
      )}
    >
      <LoginCard.Root>
        <LoginCard.Header />
        <LoginCard.EmailField />
        <LoginCard.PasswordField />
        <div className="flex items-center justify-between px-4">
          <LoginCard.RememberMe />
          <LoginCard.ForgotPassword
            onClick={asyncHandler(() =>
              router.navigate({ to: '/auth/forgot-password' }),
            )}
          />
        </div>
        <LoginCard.SubmitButton />
        <LoginCard.Separator />
        <LoginCard.SocialButtons providers={authProviders} />
        <LoginCard.Footer
          onClick={asyncHandler(() =>
            router.navigate({ to: '/auth/register' }),
          )}
        />
      </LoginCard.Root>
    </ElectronLoginFeature>
  );
}
