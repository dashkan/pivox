import {
  buildBrokerCredential,
  resolveSsoProvider,
} from '@pivox/features/broker';
import { useLogin } from '@pivox/features/login';
import { LoginCard } from '@pivox/ui/login-card';
import { getAuth, signInWithCredential } from 'firebase/auth';
import { useEffect, useState } from 'react';

import type { PivoxAuthProvider } from '@pivox/ui/auth';
import type { User } from 'firebase/auth';
import type { ReactNode } from 'react';

// Firebase provider id (what the LoginCard emits) -> broker provider
// path segment. SSO providers already arrive as `oidc.<slug>`.
const BROKER_PROVIDER: Record<string, string> = {
  'google.com': 'google',
  'github.com': 'github',
};

function brokerErrorMessage(code: string): string {
  switch (code) {
    case 'access_denied':
      return 'Sign-in was cancelled.';
    case 'auth_timeout':
      return 'Sign-in timed out. Please try again.';
    default:
      return 'Sign-in could not be completed. Please try again.';
  }
}

/**
 * Electron login. Social and SSO sign-in run through the OAuth broker
 * (system browser + the main process's loopback transport) — Firebase's
 * popup cannot run under file://, and the broker path is identical in
 * dev and packaged builds. Email/password stays on the shared useLogin
 * path: the Firebase SDK handles it directly, no browser needed.
 */
export function ElectronLoginFeature({
  onSuccess,
  onLinkRequired,
  children,
}: {
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: ReactNode;
}) {
  const value = useLogin(onSuccess, onLinkRequired);
  const [brokerError, setBrokerError] = useState<string | null>(null);
  const [brokerBaseUrl, setBrokerBaseUrl] = useState('');

  useEffect(() => {
    void window.api
      .getBrokerBaseUrl()
      .then(setBrokerBaseUrl)
      .catch(() => {
        // Base URL unavailable — SSO resolution will surface the error.
      });
  }, []);

  const signInViaBroker = async (
    provider: string,
    loginHint?: string,
  ): Promise<void> => {
    setBrokerError(null);
    const result = await window.api.startBrokerLogin(
      loginHint ? { provider, loginHint } : { provider },
    );
    if (!result.ok) {
      setBrokerError(brokerErrorMessage(result.error));
      return;
    }
    try {
      const credential = await signInWithCredential(
        getAuth(),
        buildBrokerCredential(result),
      );
      onSuccess?.(credential.user);
    } catch {
      setBrokerError('Sign-in could not be completed. Please try again.');
    }
  };

  const overridden = {
    ...value,
    state: { ...value.state, error: brokerError ?? value.state.error },
    actions: {
      ...value.actions,
      socialLogin: async (provider: PivoxAuthProvider): Promise<void> => {
        await signInViaBroker(BROKER_PROVIDER[provider] ?? provider);
      },
      ssoLogin: async (): Promise<void> => {
        setBrokerError(null);
        try {
          const providerId = await resolveSsoProvider(
            value.state.email,
            brokerBaseUrl,
          );
          if (!providerId) {
            setBrokerError(
              'No single sign-on is configured for that email domain.',
            );
            return;
          }
          await signInViaBroker(providerId, value.state.email);
        } catch {
          setBrokerError(
            'Could not reach the sign-in service. Please try again.',
          );
        }
      },
    },
  };

  return <LoginCard.Provider value={overridden}>{children}</LoginCard.Provider>;
}
