'use client';

import { asyncHandler, reportError } from '@pivox/observability';
import { LAST_EMAIL, storage } from '@pivox/storage';
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

import {
  applyRememberMeAfterSignIn,
  type SignInMethod,
} from '@/login/remember-email';
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
  /**
   * Awaitable so consumers can do server-side work (e.g. mint a
   * Firebase session cookie) before navigation. Sync handlers still
   * satisfy the `void | Promise<void>` union — Electron passes a
   * plain sync handler, start awaits a `createSession` round-trip.
   */
  onSuccess?: (user: User) => void | Promise<void>;
  onLinkRequired?: (email: string) => void;
  /**
   * SSR-resolved auto-fill email from the `pivox.login.last-email`
   * cookie. Threaded by the route so the email field paints with
   * the saved value on first render — without this, the field
   * would flicker from empty → filled when client hydration reads
   * the cookie/localStorage after mount.
   *
   * Optional because non-SSR consumers (electron) don't supply one;
   * the lazy initializer below falls back to a client-side cookie
   * read in that case.
   */
  initialEmail?: string | null;
}): LoginContextValue {
  const {
    transport,
    step,
    onStepChange,
    onSuccess,
    onLinkRequired,
    initialEmail,
  } = input;
  const emailRef = useRef<HTMLInputElement | null>(null);
  // Initialize email synchronously from the highest-priority source
  // available at render time:
  //   1. SSR-resolved value (matches the HTML the server rendered —
  //      no hydration mismatch)
  //   2. Client-side storage (electron + any pure-CSR consumer)
  //   3. Empty string
  // `storage.get` is SSR-safe (short-circuits when document/window
  // are undefined).
  const [email, setEmail] = useState<string>(
    () => initialEmail ?? storage.get(LAST_EMAIL) ?? '',
  );
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  // Defaults to true. We don't flip `rememberEmail` based on whether
  // a saved email exists — the user's intent (default-on) shouldn't
  // change on the basis of "did we have one stored from a prior
  // session." If they want to opt out, they uncheck.
  const [rememberEmail, setRememberEmail] = useState(true);
  // Broker-flow lifecycle. The controller is recreated per flow (an
  // AbortController is single-use — once aborted, it can't be reset),
  // and the boolean drives UI affordances (disabled inputs + Cancel
  // button). Kept in a ref because we never want the controller's
  // identity to trigger a re-render — only `brokerInFlight` should.
  const [brokerInFlight, setBrokerInFlight] = useState(false);
  const brokerAbortRef = useRef<AbortController | null>(null);
  // Timestamp of the most recent broker CANCELLATION (popup_closed —
  // either from explicit Cancel-button or from the
  // `BrowserRedirectTransport` closed-poll auto-settling on
  // background popup close). The form action swallows submits that
  // fire within ~250ms because those are race clicks landing on the
  // freshly-rendered Submit button as it replaces Cancel. Real
  // "Continue" clicks land seconds later, well past the window.
  //
  // The guard arms ONLY for cancellation resolutions, not success or
  // unrelated errors. Two reasons:
  //   1. Success: onSuccess navigates away in normal flows, so the
  //      guard would be invisible — but a future success path that
  //      DOESN'T navigate (e.g., pick-an-org step, MFA challenge)
  //      would leave the form silently dead for 250ms.
  //   2. Enter-key retry race: a user who reads the cancellation
  //      error and presses Enter to retry could be inside the window
  //      with no feedback for the swallowed submit.
  //
  // Electron doesn't have the underlying race because there's no
  // equivalent background-close signal for the OS browser — broker
  // waits for explicit IPC abort or 5-min timeout. But the same flag
  // applies uniformly across both transports for simplicity.
  const brokerCancelledAtRef = useRef(0);

  // Orphan-URL recovery: if we landed on `?step=password` without an
  // email in state (no SSR value, no cookie, no localStorage), bounce
  // back to the email step — the password step has no meaning
  // without prior email submission.
  //
  // Lazy-init for email above means we can read it directly here
  // rather than the older split-hydration pattern. `replace: true`
  // because this isn't a user-intended navigation; we shouldn't
  // push a new history entry the user has to back out of.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    if (step === 'password' && !email) {
      onStepChange('email', { replace: true });
    }
    // Intentionally [] — one-shot mount-time URL reconciliation. The
    // forward step transition (email → password) only runs after a
    // user submits a valid email, so we don't need to re-check.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // onSuccess wrapper that applies the remember-me policy before
  // forwarding to the caller-supplied callback. Method-aware so the
  // policy can differ between password (respect the checkbox) and
  // social/SSO (always clear) — see `applyRememberMeAfterSignIn` for
  // the full spec.
  //
  // Intentionally NOT wrapped in `useCallback`: the closure must
  // capture the LATEST `rememberEmail` at success time (the user may
  // toggle the checkbox while an async sign-in is in flight). A
  // stable identity from useCallback would freeze that closure at
  // mount and lose post-toggle changes. We persist on SUCCESS only —
  // typos, dismissed SSO popups, and wrong-password attempts
  // shouldn't poison the slot.
  const handleSuccess = async (
    user: User,
    method: SignInMethod,
  ): Promise<void> => {
    applyRememberMeAfterSignIn({
      method,
      email: user.email ?? email,
      rememberEmail,
    });
    await onSuccess?.(user);
  };

  // Lifecycle bracket for the broker flow. Wrap a broker-driven sign-
  // in so brokerInFlight + the AbortController are managed in one
  // place — every entry path sets it true with a fresh controller,
  // every exit path (success, failure, cancellation, throw) clears
  // it. Keeping the bracket here means the call sites stay readable
  // and we can't forget to clean up on the failure branches.
  const withBrokerFlow = async (
    fn: (signal: AbortSignal) => Promise<void>,
  ): Promise<void> => {
    // Defensive — if a prior flow's controller somehow lingered (e.g.
    // a hot-reload mid-flow), abort it so we don't end up with two
    // popups racing.
    brokerAbortRef.current?.abort();
    const controller = new AbortController();
    brokerAbortRef.current = controller;
    setBrokerInFlight(true);
    try {
      await fn(controller.signal);
    } finally {
      // Only clear the in-flight flag if THIS controller is still the
      // active one. If the user clicked Cancel AND then immediately
      // started another flow, the second `withBrokerFlow` invocation
      // replaced the ref; we don't want this finally to clobber the
      // newer flow's state.
      if (brokerAbortRef.current === controller) {
        brokerAbortRef.current = null;
        // Arm the race-guard timestamp only if the resolution was a
        // cancellation (signal aborted before settlement). Success
        // paths and unrelated errors don't have the popup-poll race,
        // so they don't need the guard — and skipping arming for
        // them keeps the form responsive in non-cancellation flows.
        if (controller.signal.aborted) {
          brokerCancelledAtRef.current = Date.now();
        }
        setBrokerInFlight(false);
      }
    }
  };

  const cancelBrokerFlow = (): void => {
    brokerAbortRef.current?.abort();
  };

  // useActionState captures `step`/`email`/`password` per render; on
  // submit React invokes the latest closure, so the freshly-set step
  // is visible inside.
  //
  // The body is wrapped in a try/catch + reportError to match
  // socialLogin's `asyncHandler` shape. socialLogin uses asyncHandler
  // directly (canonical onClick pattern: returns void); formAction
  // can't because useActionState's signature requires a return value,
  // so we do the equivalent inline. Both paths route unexpected
  // throws to observability — without this, SSO failures escaping
  // signInViaBroker / withBrokerFlow would be silently swallowed by
  // React's transition error boundary.
  const [, formAction] = useActionState(async () => {
    try {
      // Race guard: a form submit fired within 250ms of a broker
      // CANCELLATION is almost certainly a click that landed on the
      // Submit button as it replaced the Cancel button (see
      // brokerCancelledAtRef above). Real submits arrive seconds
      // later, well past the window. Swallow silently — the error
      // message from the broker resolution is still on screen telling
      // the user what happened.
      if (Date.now() - brokerCancelledAtRef.current < 250) return;
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
          await withBrokerFlow((signal) =>
            signInViaBroker(
              transport,
              { provider: providerId, loginHint: trimmed, signal },
              {
                onSuccess: (user) => handleSuccess(user, 'sso'),
                onLinkRequired,
                setError,
              },
            ),
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
      // Narrow the firebase-error try/catch to just the
      // signInWithEmailAndPassword call. handleSuccess sits outside
      // because its throws are NOT Firebase auth errors —
      // session-cookie minting failures, server fetch errors, etc.
      // — and passing them to firebaseErrorMessage would surface a
      // generic "unknown error" while swallowing the real cause.
      // Throws from handleSuccess fall through to the outer catch
      // and reach reportError as the programming-bug surface they
      // actually are.
      let credentialUser;
      try {
        const credential = await signInWithEmailAndPassword(
          getAuth(),
          email,
          password,
        );
        credentialUser = credential.user;
      } catch (e) {
        setError(firebaseErrorMessage(e));
        return;
      }
      await handleSuccess(credentialUser, 'password');
    } catch (err) {
      reportError(err, { source: 'useLogin.formAction' });
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

  const state: LoginState = {
    email,
    password,
    error,
    step,
    rememberEmail,
    brokerInFlight,
  };

  const actions: LoginActions = {
    updateEmail,
    updatePassword: setPassword,
    setRememberEmail,
    formAction,
    // eslint-disable-next-line react-hooks/refs -- The async handler reads `brokerAbortRef.current` via `withBrokerFlow`, but it only runs when the user clicks a social button (post-render), not during render. The lint rule traces the static call graph and can't distinguish "captured for later" from "invoked now."
    socialLogin: asyncHandler(async (provider) => {
      setError(null);
      await withBrokerFlow((signal) =>
        signInViaBroker(
          transport,
          { provider: BROKER_PROVIDER[provider] ?? provider, signal },
          {
            onSuccess: (user) => handleSuccess(user, 'social'),
            onLinkRequired,
            setError,
          },
        ),
      );
    }),
    cancelBrokerFlow,
  };

  const meta: LoginMeta = { emailRef };

  return { state, actions, meta };
}
