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
        disabled={pending}
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
    <Field className={cn('px-4', className)}>
      <FieldLabel>Password</FieldLabel>
      <Input
        name="password"
        type="password"
        autoComplete="current-password"
        autoFocus
        value={state.password}
        onChange={(e) => {
          actions.updatePassword(e.target.value);
        }}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  RememberMe                                                        */
/* ------------------------------------------------------------------ */

function LoginCardRememberMe({ className }: { className?: string }) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <Checkbox id="remember" />
      <Label htmlFor="remember" className="text-sm font-normal">
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
  const { state } = useLoginContext();
  const { pending } = useFormStatus();
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
  const { actions } = useLoginContext();
  const { pending } = useFormStatus();

  return (
    <div className={cn('flex flex-col gap-2 px-4', className)}>
      {providers.includes('google.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
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
          disabled={pending}
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
          disabled={pending}
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
