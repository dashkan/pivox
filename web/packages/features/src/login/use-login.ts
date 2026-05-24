'use client';

import { asyncHandler } from '@pivox/observability';
import { getAuth, signInWithEmailAndPassword } from 'firebase/auth';
import { useActionState, useRef, useState } from 'react';

import type {
  LoginActions,
  LoginContextValue,
  LoginMeta,
  LoginState,
  LoginStep,
} from '@pivox/ui/login-card';
import type { User } from 'firebase/auth';

import { BROKER_PROVIDER, signInViaBroker } from '@/shared/broker-auth';
import { firebaseErrorMessage } from '@/shared/firebase-error';

import type { RedirectTransport } from '@/shared/redirect-transport';

/**
 * Login state machine — email-first.
 *
 *   step 'email'    submit → transport.resolveSsoProvider(email)
 *                    ↳ providerId → signInViaBroker (SSO/OIDC)
 *                    ↳ null       → step 'password' (no SSO; collect password)
 *                    ↳ error      → generic message, stay on step 1
 *
 *   step 'password' submit → signInWithEmailAndPassword
 *
 * Editing the email after step 1 rolls the state back to step 'email'
 * — they may be typing a different domain, so the previous SSO/password
 * decision is stale. Mirrors the SwiftUI native LoginView (see
 * native/platform/macos/swift/Auth/LoginView.swift).
 *
 * Social and SSO sign-in (both branches of `signInViaBroker`) go
 * through the injected `transport` — a browser popup on the web, a
 * loopback / custom-scheme flow in Electron — so this hook stays
 * transport-agnostic.
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
  const [step, setStep] = useState<LoginStep>('email');

  // useActionState captures `step`/`email`/`password` per render; on
  // submit React invokes the latest closure, so the freshly-set step
  // is visible inside.
  const [, formAction] = useActionState(async () => {
    setError(null);
    if (step === 'email') {
      const trimmed = email.trim();
      if (!trimmed) return;
      let providerId: string | null;
      try {
        providerId = await transport.resolveSsoProvider(trimmed);
      } catch {
        setError("Couldn't reach the sign-in service. Please try again.");
        return;
      }
      if (providerId) {
        await signInViaBroker(
          transport,
          { provider: providerId, loginHint: trimmed },
          { onSuccess, onLinkRequired, setError },
        );
        return;
      }
      // No SSO for this domain — reveal the password field. We do NOT
      // surface "no account exists" here: `:resolveProvider` is a
      // public endpoint with anti-enumeration shape, and the password
      // path's invalid-credentials response already covers the
      // bad-account case with the same non-disclosing message.
      setStep('password');
      return;
    }
    // step === 'password'
    if (!password) return;
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

  // Editing the email after the SSO resolve invalidates the decision
  // (different domain may have different SSO config or none at all).
  // Drop back to step 'email' and clear the password so a stale value
  // can't be submitted against the new email.
  const updateEmail = (next: string): void => {
    setEmail(next);
    if (step === 'password') {
      setStep('email');
      setPassword('');
    }
  };

  const state: LoginState = { email, password, error, step };

  const actions: LoginActions = {
    updateEmail,
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
  };

  const meta: LoginMeta = { emailRef };

  return { state, actions, meta };
}
