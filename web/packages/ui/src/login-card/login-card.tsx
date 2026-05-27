'use client';

import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { Checkbox } from '@pivox/primitives/checkbox';
import { Field, FieldError, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { Label } from '@pivox/primitives/label';
import { Separator } from '@pivox/primitives/separator';
import { cn } from '@pivox/primitives/utils';
import { useEffect, useRef } from 'react';
import { useFormStatus } from 'react-dom';

import { LoginContext, useLoginContext } from './login-card.context';

import type { LoginContextValue } from './login-card.types';
import type { PivoxAuthProvider } from '../shared/auth-provider';

import { AppleIcon, GitHubIcon, GoogleIcon } from '@/shared/social-icons';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function LoginCardProvider({
  value,
  children,
}: {
  value: LoginContextValue;
  children: React.ReactNode;
}) {
  return <LoginContext value={value}>{children}</LoginContext>;
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function LoginCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useLoginContext();
  return (
    <div
      className={cn(
        'flex min-h-screen items-center justify-center p-4',
        className,
      )}
    >
      <Card className="w-full max-w-sm">
        <form action={actions.formAction} className="flex flex-col gap-4">
          {children}
        </form>
      </Card>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Header                                                            */
/* ------------------------------------------------------------------ */

function LoginCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Sign in</CardTitle>
      <CardDescription>Sign in to your account</CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  EmailField                                                        */
/* ------------------------------------------------------------------ */

function LoginCardEmailField({ className }: { className?: string }) {
  const { state, actions, meta } = useLoginContext();
  const { pending } = useFormStatus();
  // Disable while a broker (social/SSO) flow is in flight too. The
  // user has committed to a provider — typing here while waiting on
  // a popup creates confusion (does the popup carry the new value?
  // no, it doesn't) and accepting input we'd then have to reconcile.
  // Same rule applies to every input on the card; the Cancel button
  // is the only enabled control during a broker flow.
  const disabled = pending || state.brokerInFlight;
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Email</FieldLabel>
      <Input
        // eslint-disable-next-line react-hooks/refs -- forwarding the ref object to a JSX `ref={}` prop, not reading `.current` during render
        ref={meta.emailRef}
        name="email"
        type="email"
        placeholder="name@example.com"
        autoComplete="email"
        value={state.email}
        onChange={(e) => {
          actions.updateEmail(e.target.value);
        }}
        disabled={disabled}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  PasswordField                                                     */
/* ------------------------------------------------------------------ */

function LoginCardPasswordField({ className }: { className?: string }) {
  const { state, actions } = useLoginContext();
  const { pending } = useFormStatus();
  // Conditional MOUNT, not just hidden — keeps password managers from
  // prompting during the email-only step. Matches the SwiftUI native
  // LoginView (see native/platform/macos/swift/Auth/LoginView.swift).
  if (state.step !== 'password') return null;
  return (
    <LoginCardPasswordInput
      state={state}
      actions={actions}
      pending={pending || state.brokerInFlight}
      className={className}
    />
  );
}

// Inner component scoped to the password step so the focus effect
// fires exactly once on mount (the parent only renders this child
// during the password step). Two reasons we bypass the obvious
// `autoFocus` / ref-on-Input path:
//   1. The `<Input>` primitive is the shadcn-vendored function
//      component whose ref-forwarding behavior across the `{...props}`
//      spread we don't fully control across regen.
//   2. The form is mid-submit when this component mounts —
//      useActionState flips `pending` back to false in the same commit
//      that mounts the password field, but the browser's
//      focus-restoration to the just-clicked Continue button can race
//      against React's autoFocus pass.
// requestAnimationFrame defers the focus call past the commit and
// past any focus-restoration the browser does post-submit. We scope
// the input lookup to a container ref + querySelector (instead of
// document.getElementsByName) so a hypothetical second `name="password"`
// elsewhere in the DOM — global search bar with a password field,
// future overlay — can't ever steal focus from us.
function LoginCardPasswordInput({
  state,
  actions,
  pending,
  className,
}: {
  state: ReturnType<typeof useLoginContext>['state'];
  actions: ReturnType<typeof useLoginContext>['actions'];
  pending: boolean;
  className?: string;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const raf = window.requestAnimationFrame(() => {
      const el = container.querySelector<HTMLInputElement>(
        'input[name="password"]',
      );
      el?.focus();
    });
    return () => {
      window.cancelAnimationFrame(raf);
    };
  }, []);
  return (
    <div ref={containerRef}>
      <Field className={cn('px-4', className)}>
        <FieldLabel>Password</FieldLabel>
        <Input
          name="password"
          type="password"
          autoComplete="current-password"
          value={state.password}
          onChange={(e) => {
            actions.updatePassword(e.target.value);
          }}
          disabled={pending}
        />
      </Field>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  RememberMe                                                        */
/* ------------------------------------------------------------------ */

function LoginCardRememberMe({ className }: { className?: string }) {
  const { state, actions } = useLoginContext();
  const { pending } = useFormStatus();
  // The remember-me preference applies on success — toggling it mid-
  // broker-flow has no defined semantics (does it apply to the
  // in-flight flow's outcome? to the next flow?). Locking the
  // checkbox while a flow is in flight makes the contract obvious:
  // "your choice was committed when you started the flow."
  const disabled = pending || state.brokerInFlight;
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <Checkbox
        id="remember"
        checked={state.rememberEmail}
        disabled={disabled}
        // Radix's CheckedState is `boolean | 'indeterminate'`. We never
        // use the indeterminate tri-state for this control, so collapse
        // anything non-true to false.
        onCheckedChange={(next) => {
          actions.setRememberEmail(next === true);
        }}
      />
      <Label
        htmlFor="remember"
        className={cn('text-sm font-normal', disabled && 'opacity-50')}
      >
        Remember me
      </Label>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  ForgotPassword                                                    */
/* ------------------------------------------------------------------ */

function LoginCardForgotPassword({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  const { state } = useLoginContext();
  // Only relevant once the user is past the email step.
  if (state.step !== 'password') return null;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'text-sm text-muted-foreground underline-offset-4 hover:text-primary hover:underline',
        className,
      )}
    >
      Forgot password?
    </button>
  );
}

/* ------------------------------------------------------------------ */
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function LoginCardSubmitButton({ className }: { className?: string }) {
  const { state, actions } = useLoginContext();
  const { pending } = useFormStatus();

  // Broker flow in progress: swap the submit button for a Cancel
  // button so the user has an explicit way out. type="button" so a
  // form submit doesn't fire (we want cancelBrokerFlow exclusively).
  // Cancel is always enabled — the user shouldn't be stuck waiting
  // because some other "disabled" condition would also be true.
  //
  // The "Cancel was clicked but the broker had already resolved in
  // the background and Submit took its place" race is handled at
  // the form-action level in `useLogin` (250ms swallow window after
  // broker resolution) so the UI swap stays snappy here.
  if (state.brokerInFlight) {
    return (
      <div className={cn('flex flex-col gap-4 px-4', className)}>
        {state.error && <FieldError>{state.error}</FieldError>}
        <Button
          type="button"
          variant="outline"
          className="w-full"
          onClick={() => {
            actions.cancelBrokerFlow();
          }}
        >
          Cancel sign-in
        </Button>
      </div>
    );
  }

  // Step 1 (email) needs only an email; step 2 (password) needs both.
  // The disabled mirror keeps the button from firing a no-op submit
  // before the user has filled the required fields for the step.
  const trimmedEmail = state.email.trim();
  const disabled =
    pending ||
    trimmedEmail.length === 0 ||
    (state.step === 'password' && state.password.length === 0);
  const label =
    state.step === 'password'
      ? pending
        ? 'Signing in…'
        : 'Sign in'
      : pending
        ? 'Please wait…'
        : 'Continue';
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={disabled}>
        {label}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Separator                                                         */
/* ------------------------------------------------------------------ */

function LoginCardSeparator({ className }: { className?: string }) {
  return (
    <div className={cn('relative px-4', className)}>
      <div className="absolute inset-x-4 inset-y-0 flex items-center">
        <Separator className="w-full" />
      </div>
      <div className="relative flex justify-center text-xs uppercase">
        <span className="bg-card px-2 text-muted-foreground">or</span>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  SocialButtons                                                     */
/* ------------------------------------------------------------------ */

function LoginCardSocialButtons({
  providers = ['google.com', 'github.com'] as Array<PivoxAuthProvider>,
  className,
}: {
  providers?: Array<PivoxAuthProvider>;
  className?: string;
}) {
  const { state, actions } = useLoginContext();
  const { pending } = useFormStatus();
  // Disable while ANY auth path is in flight — form submit, social,
  // or SSO. Without the brokerInFlight check, a user could start a
  // Google flow and click GitHub mid-popup, opening two competing
  // OAuth windows.
  const disabled = pending || state.brokerInFlight;

  return (
    <div className={cn('flex flex-col gap-2 px-4', className)}>
      {providers.includes('google.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={disabled}
          onClick={() => {
            actions.socialLogin('google.com');
          }}
        >
          <GoogleIcon />
          Sign in with Google
        </Button>
      )}
      {providers.includes('github.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={disabled}
          onClick={() => {
            actions.socialLogin('github.com');
          }}
        >
          <GitHubIcon />
          Sign in with GitHub
        </Button>
      )}
      {providers.includes('apple.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={disabled}
          onClick={() => {
            actions.socialLogin('apple.com');
          }}
        >
          <AppleIcon />
          Sign in with Apple
        </Button>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function LoginCardFooter({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  return (
    <CardFooter className={cn('justify-center', className)}>
      <p className="text-sm text-muted-foreground">
        Don&apos;t have an account?{' '}
        <button
          type="button"
          className="text-primary underline-offset-4 hover:underline"
          onClick={onClick}
        >
          Sign up
        </button>
      </p>
    </CardFooter>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const LoginCard = {
  Provider: LoginCardProvider,
  Root: LoginCardRoot,
  Header: LoginCardHeader,
  EmailField: LoginCardEmailField,
  PasswordField: LoginCardPasswordField,
  RememberMe: LoginCardRememberMe,
  ForgotPassword: LoginCardForgotPassword,
  SubmitButton: LoginCardSubmitButton,
  Separator: LoginCardSeparator,
  SocialButtons: LoginCardSocialButtons,
  Footer: LoginCardFooter,
  Context: LoginContext,
};
