import { LoginFeature } from '@pivox/features/login';
import { asyncHandler } from '@pivox/observability';
import { LoginCard } from '@pivox/ui/login-card';
import {
  createFileRoute,
  useNavigate,
  useRouter,
} from '@tanstack/react-router';

import type { LoginStep } from '@pivox/ui/login-card';

import { authProviders } from '@/lib/auth-providers';
import { browserRedirectTransport } from '@/lib/browser-redirect-transport';
import { createSession } from '@/server/auth-session';

/**
 * Search schema. `step` lives in the URL so the browser back button
 * pops password → email naturally instead of skipping the whole
 * login flow (everything-in-one-component would be invisible to
 * history). Unknown / malformed values fall back to 'email' — the
 * password step has no meaning without prior email submission.
 *
 * `step` is OPTIONAL in the search type so external callers can
 * `navigate({ to: '/auth/login' })` without knowing about the param;
 * the route defaults to 'email' when absent. The validator
 * canonicalizes — anything other than 'password' is dropped, so
 * `/auth/login?step=email` and `/auth/login?step=garbage` both land
 * at the same empty-search canonical URL.
 *
 * Email is intentionally NOT in the URL: it would leak into browser
 * history, server access logs, and Referer headers when the user
 * follows an off-site link from the login screen. Re-submission is
 * cheap; privacy is not.
 */
type LoginSearch = {
  step?: LoginStep;
  /**
   * Post-login destination. Set by `_app`'s beforeLoad when an
   * unauthenticated visit gets redirected here, preserved through
   * the flow, and navigated to on successful sign-in. Validator
   * enforces same-origin (must start with '/') so a forged
   * `?return=https://evil.com` can't turn login into an open redirect.
   */
  return?: string;
};

export const Route = createFileRoute('/auth/login')({
  validateSearch: (search: Record<string, unknown>): LoginSearch => {
    const out: LoginSearch = {};
    if (search.step === 'password') out.step = 'password';
    // Path-relative + reject `//host/path` (protocol-relative URL —
    // `startsWith('/')` returns true but browsers treat it as
    // cross-origin, which would be an open redirect).
    if (
      typeof search.return === 'string' &&
      search.return.startsWith('/') &&
      !search.return.startsWith('//')
    ) {
      out.return = search.return;
    }
    return out;
  },
  component: LoginPage,
});

function LoginPage() {
  const router = useRouter();
  const navigate = useNavigate({ from: '/auth/login' });
  const { step = 'email', return: returnUrl } = Route.useSearch();

  return (
    <LoginFeature
      transport={browserRedirectTransport}
      step={step}
      // `replace: true` is forwarded by the hook for auto-corrections
      // (rollback on email edit, refresh fallback). Forward transitions
      // (email → password) default to push, which is what gives back-
      // nav the email step.
      // For step 'email' we OMIT the param entirely so the URL stays
      // at the canonical /auth/login (matches an absent step in the
      // validator). For 'password' we set it explicitly.
      onStepChange={(nextStep, opts) =>
        void navigate({
          search: nextStep === 'password' ? { step: 'password' } : {},
          replace: opts?.replace,
        })
      }
      // Mint the server-side session cookie BEFORE navigating — the
      // destination route's beforeLoad reads the cookie via
      // `getServerSession`, so without this round-trip the user would
      // hit the gate with an empty cookie and bounce back here.
      onSuccess={async (user) => {
        const idToken = await user.getIdToken();
        await createSession({ data: { idToken } });
        await router.navigate({ to: returnUrl ?? '/' });
      }}
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
