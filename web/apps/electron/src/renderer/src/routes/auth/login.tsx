import { LoginFeature } from '@pivox/features/login';
import { asyncHandler } from '@pivox/observability';
import { LoginCard } from '@pivox/ui/login-card';
import { authProviders } from '@renderer/lib/auth-providers';
import { electronRedirectTransport } from '@renderer/lib/electron-redirect-transport';
import { createFileRoute, useNavigate, useRouter } from '@tanstack/react-router';

import type { LoginStep } from '@pivox/ui/login-card';

/**
 * Search schema. `step` lives in the URL so the back button pops
 * password → email naturally — mirror of the start-app route. See
 * web/apps/start/src/routes/auth/login.tsx for the full rationale,
 * including why `step` is optional and email is never in the URL.
 */
type LoginSearch = {
  step?: LoginStep;
};

export const Route = createFileRoute('/auth/login')({
  validateSearch: (search: Record<string, unknown>): LoginSearch =>
    search.step === 'password' ? { step: 'password' } : {},
  component: LoginPage,
});

function LoginPage() {
  const router = useRouter();
  const navigate = useNavigate({ from: '/auth/login' });
  const { step = 'email' } = Route.useSearch();

  return (
    <LoginFeature
      transport={electronRedirectTransport}
      step={step}
      onStepChange={(nextStep, opts) =>
        void navigate({
          search: nextStep === 'password' ? { step: 'password' } : {},
          replace: opts?.replace,
        })
      }
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
    </LoginFeature>
  );
}
