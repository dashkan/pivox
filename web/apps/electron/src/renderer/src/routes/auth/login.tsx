import { useAuth } from '@pivox/features/auth';
import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

export const Route = createFileRoute('/auth/login')({
  component: SignInPage,
});

/**
 * Sign-in entry. Desktop auth follows RFC 8252: there are no in-app credential
 * fields — the single action hands off to the system browser, where Keycloak's
 * own themed pages handle login, registration, password reset, social, and
 * enterprise SSO. The main process runs the PKCE flow and, on success, flips the
 * auth state; the effect below then leaves this screen.
 */
function SignInPage() {
  const router = useRouter();
  const { user, loading } = useAuth();
  const [signingIn, setSigningIn] = useState(false);
  const [failed, setFailed] = useState(false);

  // Already signed in (or the flow just completed) → leave the sign-in screen.
  useEffect(() => {
    if (!loading && user) void router.navigate({ to: '/' });
  }, [loading, user, router]);

  const signIn = async (): Promise<void> => {
    setFailed(false);
    setSigningIn(true);
    try {
      const result = await window.api.login();
      // Success flows through the auth:changed event → the effect navigates.
      // Cancel (user dismissed) and login_in_progress (a redundant concurrent
      // invoke) are silent; any real failure shows a retry hint.
      const silent = result.error === 'cancelled' || result.error === 'login_in_progress';
      if (!result.ok && !silent) setFailed(true);
    } catch {
      // login() should resolve a LoginResult, but guard a rejected IPC call so
      // it doesn't become an unhandled rejection with no user feedback.
      setFailed(true);
    } finally {
      setSigningIn(false);
    }
  };

  // Don't flash the sign-in card while boot restore is still resolving — an
  // already-signed-in user would otherwise see it before the effect navigates.
  if (loading) return null;

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-xl">Welcome to Pivox</CardTitle>
          <CardDescription>
            Sign in through your browser to continue.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Button
            className="w-full"
            disabled={signingIn}
            onClick={() => {
              void signIn();
            }}
          >
            {signingIn ? 'Waiting for browser…' : 'Sign in with Pivox'}
          </Button>
          {signingIn ? (
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                void window.api.cancelLogin();
              }}
            >
              Cancel
            </Button>
          ) : null}
          {failed ? (
            <p className="text-center text-sm text-destructive">
              Sign-in didn’t complete. Please try again.
            </p>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
