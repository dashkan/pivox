'use client';

import { asyncHandler } from '@pivox/observability';
import { getAuth, signInWithEmailAndPassword } from 'firebase/auth';
import { useActionState, useRef, useState } from 'react';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type {
  LoginActions,
  LoginContextValue,
  LoginMeta,
  LoginState,
} from '@pivox/ui/login-card';
import type { User } from 'firebase/auth';

import { BROKER_PROVIDER, signInViaBroker } from '@/shared/broker-auth';
import { firebaseErrorMessage } from '@/shared/firebase-error';


/**
 * Login state machine. Email/password goes straight through the
 * Firebase SDK; social and SSO sign-in run through the OAuth broker via
 * the injected `transport` — a browser popup on the web, a loopback /
 * custom-scheme flow in Electron. The feature stays transport-agnostic
 * so the same hook drives both apps.
 */
export function useLogin(input: {
  transport: RedirectTransport;
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
}): LoginContextValue {
  const { transport, onSuccess, onLinkRequired } = input;
  const emailRef = useRef<HTMLInputElement | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const [, formAction] = useActionState(async () => {
    setError(null);
    try {
      const credential = await signInWithEmailAndPassword(
        getAuth(),
        email,
        password,
      );
      onSuccess?.(credential.user);
    } catch (e) {
      setError(firebaseErrorMessage(e));
    }
  }, null);

  const state: LoginState = { email, password, error };

  const actions: LoginActions = {
    updateEmail: setEmail,
    updatePassword: setPassword,
    formAction,

    socialLogin: asyncHandler(async (provider) => {
      setError(null);
      await signInViaBroker(
        transport,
        { provider: BROKER_PROVIDER[provider] ?? provider },
        { onSuccess, onLinkRequired, setError },
      );
    }),

    ssoLogin: asyncHandler(async () => {
      setError(null);
      let providerId: string | null;
      try {
        providerId = await transport.resolveSsoProvider(email);
      } catch {
        setError('Could not reach the sign-in service. Please try again.');
        return;
      }
      if (!providerId) {
        setError('No single sign-on is configured for that email domain.');
        return;
      }
      // login_hint pre-fills the account picker at the IdP so the user
      // doesn't retype the email they already entered.
      await signInViaBroker(
        transport,
        { provider: providerId, loginHint: email },
        { onSuccess, onLinkRequired, setError },
      );
    }),
  };

  const meta: LoginMeta = { emailRef };

  return { state, actions, meta };
}
