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
 * localStorage key for the auto-fill email. Namespaced under `pivox.`
 * so a future broader settings vocabulary doesn't collide. Stored as
 * a plain string — no JSON envelope — because the value IS the slot.
 */
const LAST_EMAIL_STORAGE_KEY = 'pivox.login.last-email';

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
  /**
   * Awaitable so consumers can do server-side work (e.g. mint a
   * Firebase session cookie) before navigation. Sync handlers still
   * satisfy the `void | Promise<void>` union — Electron passes a
   * plain sync handler, start awaits a `createSession` round-trip.
   */
  onSuccess?: (user: User) => void | Promise<void>;
  onLinkRequired?: (email: string) => void;
}): LoginContextValue {
  const { transport, step, onStepChange, onSuccess, onLinkRequired } = input;
  const emailRef = useRef<HTMLInputElement | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  // Defaults to true. The auto-fill email is read from localStorage in
  // a mount effect (NOT a lazy useState initializer) so the hook is
  // SSR-safe: server render sees the default empty/true state, the
  // client mounts and hydrates with the matching default, then the
  // effect populates from localStorage. Avoids hydration mismatch.
  const [rememberEmail, setRememberEmail] = useState(true);

  // Single mount-time effect that BOTH hydrates from localStorage AND
  // decides whether the URL is an orphaned `?step=password`. Kept as
  // one effect deliberately: splitting hydration + fallback into two
  // `[]`-deps effects creates an effect-order race — the fallback
  // reads `email` from the current render snapshot ('' on first
  // render), the hydration's setState is batched into the next
  // render, so a returning user with a stored email + an orphaned
  // password URL would get bounced to the email step even though we
  // could have continued the flow on the password step. Reading
  // `saved` locally here lets the fallback see the about-to-be-set
  // email and only redirect when there's truly no context.
  //
  // The obvious alternative — a lazy `useState(() => localStorage…)`
  // initializer — would break SSR: the server renders with '' (no
  // window), the client hydrates with the stored email, and React
  // throws a hydration mismatch. Effect-based hydration is the
  // SSR-safe shape, and the synchronous setState is bounded to a
  // single one-shot read on mount — exactly the case
  // `set-state-in-effect` is designed to permit but the linter can't
  // statically prove.
  //
  // We don't flip `rememberEmail` based on whether a saved email
  // exists — the user's intent (default-on) shouldn't change on the
  // basis of "did we have one stored from a prior session." If they
  // want to opt out, they uncheck.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const saved = window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot localStorage hydration; lazy initializer is SSR-unsafe
    if (saved) setEmail(saved);
    if (step === 'password' && !saved) {
      onStepChange('email', { replace: true });
    }
    // Intentionally [] — one-shot mount-time URL reconciliation +
    // hydration. The forward step transition (email → password) sets
    // email first inside the same action, so this effect must not
    // re-fire and bounce us back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /**
   * Apply the user's remember-me preference at sign-in time. Called
   * from the success branches of password, SSO, and social flows.
   *
   *   rememberEmail=true  + email present → write the slot
   *   rememberEmail=false                  → clear the slot (the user
   *                                          explicitly opted out; any
   *                                          previously stored value
   *                                          is wiped, including from
   *                                          a different sign-in path)
   *   rememberEmail=true  + email empty    → no-op (defensive — every
   *                                          known sign-in path
   *                                          carries an email today)
   *
   * We persist on SUCCESS only — typos, dismissed SSO popups, and
   * wrong-password attempts shouldn't poison the slot. No `typeof
   * window` guard needed: this only runs from async sign-in handlers,
   * which only execute in the browser.
   */
  const persistEmailPreference = (signedInEmail: string): void => {
    const trimmed = signedInEmail.trim();
    if (rememberEmail) {
      if (trimmed) {
        window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, trimmed);
      }
    } else {
      window.localStorage.removeItem(LAST_EMAIL_STORAGE_KEY);
    }
  };

  // onSuccess wrapper that fires the localStorage write before
  // forwarding to the caller-supplied callback. Both the password and
  // SSO/social paths route success through here so the persistence
  // rule lives in one place.
  //
  // Intentionally NOT wrapped in `useCallback`: the closure must
  // capture the LATEST `rememberEmail` at success time (the user may
  // toggle the checkbox while an async sign-in is in flight). A
  // stable identity from useCallback would freeze that closure at
  // mount and lose post-toggle changes.
  const handleSuccess = async (user: User): Promise<void> => {
    persistEmailPreference(user.email ?? email);
    await onSuccess?.(user);
  };

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
          { onSuccess: handleSuccess, onLinkRequired, setError },
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
      await handleSuccess(credential.user);
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

  const state: LoginState = { email, password, error, step, rememberEmail };

  const actions: LoginActions = {
    updateEmail,
    updatePassword: setPassword,
    setRememberEmail,
    formAction,
    socialLogin: asyncHandler(async (provider) => {
      setError(null);
      await signInViaBroker(
        transport,
        { provider: BROKER_PROVIDER[provider] ?? provider },
        { onSuccess: handleSuccess, onLinkRequired, setError },
      );
    }),
  };

  const meta: LoginMeta = { emailRef };

  return { state, actions, meta };
}
