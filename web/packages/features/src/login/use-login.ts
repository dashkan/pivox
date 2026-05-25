'use client';

import { asyncHandler } from '@pivox/observability';
import { getAuth, signInWithEmailAndPassword } from 'firebase/auth';
import { useActionState, useEffect, useRef, useState } from 'react';

import type { RedirectTransport } from '@/shared/redirect-transport';
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
 * **Step is controlled by the caller via `step` + `onStepChange`.** The
 * route lifts step into the URL search (`?step=password`) so the
 * browser back button moves password→email naturally — a single
 * mutating-state component would make the whole flow invisible to
 * history. The hook treats `step` as authoritative and never holds an
 * internal copy.
 *
 * `onStepChange` takes an optional `{ replace: true }` to mark
 * auto-corrections (rollback on email edit, refresh-fallback) so they
 * don't pollute history — the user didn't intend those navigations.
 *
 * Social and SSO sign-in (both branches of `signInViaBroker`) go
 * through the injected `transport` — a browser popup on the web, a
 * loopback / custom-scheme flow in Electron — so this hook stays
 * transport-agnostic.
 */
export function useLogin(input: {
  transport: RedirectTransport;
  step: LoginStep;
  onStepChange: (step: LoginStep, opts?: { replace?: boolean }) => void;
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
}): LoginContextValue {
  const { transport, step, onStepChange, onSuccess, onLinkRequired } = input;
  const emailRef = useRef<HTMLInputElement | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  // Refresh / deep-link fallback. If the URL lands us on
  // `?step=password` with no email in component state (we always start
  // empty — email is intentionally NOT persisted in the URL for
  // privacy), the password step has no context. Push the user back to
  // the email step and `replace` the history entry so the orphaned
  // password URL doesn't sit in history. One-shot on mount; subsequent
  // step transitions are driven by the form action / updateEmail.
  useEffect(() => {
    if (step === 'password' && !email) {
      onStepChange('email', { replace: true });
    }
    // Intentionally [] — this is a one-shot mount-time URL
    // reconciliation, not a continuous invariant. The forward step
    // transition (email → password) sets email first inside the same
    // action, so this effect must not re-fire and bounce us back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
      onStepChange('password');
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
  // can't be submitted against the new email. `replace: true` because
  // this isn't a user-intended navigation — the user is editing a
  // field, not pressing Back; we shouldn't push a new history entry.
  const updateEmail = (next: string): void => {
    setEmail(next);
    if (step === 'password') {
      onStepChange('email', { replace: true });
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
