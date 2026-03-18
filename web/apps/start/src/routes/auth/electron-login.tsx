import { createFileRoute } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import {
  getAuth,
  getRedirectResult,
  GoogleAuthProvider,
  GithubAuthProvider,
  OAuthProvider,
  signInWithRedirect,
  signOut,
} from 'firebase/auth';
import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';

type ElectronLoginSearch = {
  provider: 'google' | 'github' | 'apple';
  state: string;
};

export const Route = createFileRoute('/auth/electron-login')({
  validateSearch: (search: Record<string, unknown>): ElectronLoginSearch => ({
    provider: (search.provider as ElectronLoginSearch['provider']) || 'google',
    state: (search.state as string) || '',
  }),
  component: ElectronLoginPage,
});

const providers = {
  google: () => {
    const p = new GoogleAuthProvider();
    p.setCustomParameters({ prompt: 'select_account' });
    return p;
  },
  github: () => new GithubAuthProvider(),
  apple: () => new OAuthProvider('apple.com'),
} as const;

async function exchangeToken(idToken: string): Promise<string> {
  const res = await fetch('/internal/v1/auth:exchangeToken', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${idToken}`,
    },
  });
  if (!res.ok) {
    throw new Error(`Token exchange failed: ${res.status}`);
  }
  const data = await res.json();
  return data.custom_token;
}

const REDIRECT_KEY = 'pivox:electron-login-pending';

function ElectronLoginPage() {
  const { provider, state } = Route.useSearch();
  const [status, setStatus] = useState<
    'loading' | 'redirecting' | 'error'
  >('loading');
  const [error, setError] = useState('');

  useEffect(() => {
    const auth = getAuth();
    const redirectPending = sessionStorage.getItem(REDIRECT_KEY);

    getRedirectResult(auth)
      .then(async (result) => {
        if (result) {
          sessionStorage.removeItem(REDIRECT_KEY);

          const idToken = await result.user.getIdToken();
          const customToken = await exchangeToken(idToken);

          const params = new URLSearchParams();
          params.set('token', customToken);
          params.set('state', state);
          const deepLink = `pivox://auth/callback?${params.toString()}`;

          window.location.href = deepLink;
          // Navigate to done page after launching the deep link
          setTimeout(() => { window.location.href = '/auth/done'; }, 500);
          return;
        }

        if (redirectPending) {
          sessionStorage.removeItem(REDIRECT_KEY);
          setError('Sign-in was not completed. Please try again.');
          setStatus('error');
          return;
        }

        if (!provider || !state) {
          setError('Missing provider or state parameter.');
          setStatus('error');
          return;
        }

        const authProvider = providers[provider];
        if (!authProvider) {
          setError(`Unknown provider: ${provider}`);
          setStatus('error');
          return;
        }

        // Sign out any existing session so the OAuth flow starts clean
        // and doesn't accidentally link accounts.
        await signOut(auth);
        sessionStorage.setItem(REDIRECT_KEY, '1');
        await signInWithRedirect(auth, authProvider());
      })
      .catch((e) => {
        sessionStorage.removeItem(REDIRECT_KEY);
        console.error('Electron login error:', e);
        setError(
          e instanceof Error ? e.message : 'Something went wrong. Please try again.',
        );
        setStatus('error');
      });
  }, [provider, state]);

  if (status === 'error') {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle>Login failed</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
          <div className="px-6 pb-6">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setStatus('loading');
                setError('');
                const auth = getAuth();
                const authProvider = providers[provider];
                if (authProvider) {
                  sessionStorage.setItem(REDIRECT_KEY, '1');
                  signInWithRedirect(auth, authProvider());
                }
              }}
            >
              Try again
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  if (status === 'redirecting') {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <div className="text-center text-sm text-muted-foreground">
          <p>Redirecting to Pivox desktop app...</p>
          <p className="mt-2">
            If the app didn&apos;t open,{' '}
            <button
              type="button"
              className="text-primary underline-offset-4 hover:underline"
              onClick={() => {
                const params = new URLSearchParams();
                params.set('token', new URLSearchParams(window.location.search).get('token') || '');
                params.set('state', state);
                window.location.href = `pivox://auth/callback?${params.toString()}`;
              }}
            >
              click here to try again
            </button>
            .
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">
        Signing in with {provider}...
      </p>
    </div>
  );
}
