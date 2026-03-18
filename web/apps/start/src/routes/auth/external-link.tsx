import { createFileRoute } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import {
  getAuth,
  getRedirectResult,
  GoogleAuthProvider,
  GithubAuthProvider,
  OAuthProvider,
  signInWithCustomToken,
  linkWithRedirect,
} from 'firebase/auth';
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';

type ElectronLinkSearch = {
  provider: string;
  state: string;
  code: string;
};

export const Route = createFileRoute('/auth/external-link')({
  validateSearch: (search: Record<string, unknown>): ElectronLinkSearch => ({
    provider: (search.provider as string) || '',
    state: (search.state as string) || '',
    code: (search.code as string) || '',
  }),
  component: ElectronLinkPage,
});

const providers: Record<string, () => GoogleAuthProvider | GithubAuthProvider | OAuthProvider> = {
  'google.com': () => {
    const p = new GoogleAuthProvider();
    p.setCustomParameters({ prompt: 'select_account' });
    return p;
  },
  'github.com': () => new GithubAuthProvider(),
  'apple.com': () => new OAuthProvider('apple.com'),
};

async function consumeTokenCode(code: string): Promise<string> {
  const res = await fetch('/internal/v1/auth:consumeToken', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  });
  if (!res.ok) {
    throw new Error(`Code exchange failed: ${res.status}`);
  }
  const data = await res.json();
  return data.id_token;
}

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

const REDIRECT_KEY = 'pivox:external-link-pending';

function ElectronLinkPage() {
  const { provider, state, code } = Route.useSearch();
  const [status, setStatus] = useState<'loading' | 'redirecting' | 'error'>(
    'loading',
  );
  const [error, setError] = useState('');

  useEffect(() => {
    const auth = getAuth();
    const redirectPending = sessionStorage.getItem(REDIRECT_KEY);

    getRedirectResult(auth)
      .then(async (result) => {
        if (result) {
          // Phase 2: linkWithRedirect completed successfully
          sessionStorage.removeItem(REDIRECT_KEY);

          const params = new URLSearchParams();
          params.set('state', state);
          params.set('linked', 'true');
          const deepLink = `pivox://auth/callback?${params.toString()}`;

          window.location.href = deepLink;
          // Navigate to done page after launching the deep link
          setTimeout(() => { window.location.href = '/auth/done'; }, 500);
          return;
        }

        if (redirectPending) {
          sessionStorage.removeItem(REDIRECT_KEY);
          setError('Linking was not completed. You can close this tab.');
          setStatus('error');
          return;
        }

        // Phase 1: Sign in as the current user, then link the new provider
        if (!provider || !state || !code) {
          setError('Missing required parameters.');
          setStatus('error');
          return;
        }

        const makeProvider = providers[provider];
        if (!makeProvider) {
          setError(`Unknown provider: ${provider}`);
          setStatus('error');
          return;
        }

        // Consume the opaque code to retrieve the ID token, then exchange for a custom token
        const idToken = await consumeTokenCode(code);
        const customToken = await exchangeToken(idToken);
        const credential = await signInWithCustomToken(auth, customToken);

        // Now link the new provider via redirect
        sessionStorage.setItem(REDIRECT_KEY, '1');
        await linkWithRedirect(credential.user, makeProvider());
      })
      .catch((e) => {
        sessionStorage.removeItem(REDIRECT_KEY);
        console.error('Electron link error:', e);
        setError(
          e instanceof Error
            ? e.message
            : 'Something went wrong. Please try again.',
        );
        setStatus('error');
      });
  }, [provider, state, code]);

  if (status === 'error') {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle>Linking failed</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  if (status === 'redirecting') {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <div className="text-center text-sm text-muted-foreground">
          <p>Account linked! Redirecting to Pivox desktop app...</p>
          <p className="mt-2">
            If the app didn&apos;t open,{' '}
            <button
              type="button"
              className="text-primary underline-offset-4 hover:underline"
              onClick={() => {
                const params = new URLSearchParams();
                params.set('state', state);
                params.set('linked', 'true');
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
        Linking account...
      </p>
    </div>
  );
}
