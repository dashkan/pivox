'use client';

import { useFormStatus } from 'react-dom';
import { cn } from '@pivox/primitives/utils';
import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { Input } from '@pivox/primitives/input';
import { Field, FieldError, FieldLabel } from '@pivox/primitives/field';
import { Separator } from '@pivox/primitives/separator';
import {
  RegistrationContext,
  useRegistrationContext,
} from './registration-card.context';
import type { RegistrationContextValue } from './registration-card.types';
import type { PivoxAuthProvider } from '../shared/auth-provider';
import { AppleIcon, GitHubIcon, GoogleIcon } from '@/shared/social-icons';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function RegistrationCardProvider({
  value,
  children,
}: {
  value: RegistrationContextValue;
  children: React.ReactNode;
}) {
  return <RegistrationContext value={value}>{children}</RegistrationContext>;
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function RegistrationCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useRegistrationContext();
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

function RegistrationCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Create account</CardTitle>
      <CardDescription>Sign up for a new account</CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  EmailField                                                        */
/* ------------------------------------------------------------------ */

function RegistrationCardEmailField({ className }: { className?: string }) {
  const { state, actions, meta } = useRegistrationContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Email</FieldLabel>
      <Input
        ref={meta.emailRef}
        name="email"
        type="email"
        placeholder="name@example.com"
        autoComplete="email"
        value={state.email}
        onChange={(e) => actions.updateEmail(e.target.value)}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  DisplayNameField                                                  */
/* ------------------------------------------------------------------ */

function RegistrationCardDisplayNameField({
  className,
}: {
  className?: string;
}) {
  const { state, actions } = useRegistrationContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Display name</FieldLabel>
      <Input
        name="displayName"
        type="text"
        placeholder="John Doe"
        autoComplete="name"
        value={state.displayName}
        onChange={(e) => actions.updateDisplayName(e.target.value)}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  PasswordField                                                     */
/* ------------------------------------------------------------------ */

function RegistrationCardPasswordField({ className }: { className?: string }) {
  const { state, actions } = useRegistrationContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Password</FieldLabel>
      <Input
        name="password"
        type="password"
        autoComplete="new-password"
        value={state.password}
        onChange={(e) => actions.updatePassword(e.target.value)}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  ConfirmPasswordField                                              */
/* ------------------------------------------------------------------ */

function RegistrationCardConfirmPasswordField({
  className,
}: {
  className?: string;
}) {
  const { state, actions } = useRegistrationContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Confirm password</FieldLabel>
      <Input
        name="confirmPassword"
        type="password"
        autoComplete="new-password"
        value={state.confirmPassword}
        onChange={(e) => actions.updateConfirmPassword(e.target.value)}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function RegistrationCardSubmitButton({ className }: { className?: string }) {
  const { state } = useRegistrationContext();
  const { pending } = useFormStatus();
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Please wait…' : 'Create account'}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Separator                                                         */
/* ------------------------------------------------------------------ */

function RegistrationCardSeparator({ className }: { className?: string }) {
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

function RegistrationCardSocialButtons({
  providers = ['google.com', 'github.com'] as Array<PivoxAuthProvider>,
  className,
}: {
  providers?: Array<PivoxAuthProvider>;
  className?: string;
}) {
  const { actions } = useRegistrationContext();
  const { pending } = useFormStatus();

  return (
    <div className={cn('flex flex-col gap-2 px-4', className)}>
      {providers.includes('google.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin('google.com')}
        >
          <GoogleIcon />
          Sign up with Google
        </Button>
      )}
      {providers.includes('github.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin('github.com')}
        >
          <GitHubIcon />
          Sign up with GitHub
        </Button>
      )}
      {providers.includes('apple.com') && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin('apple.com')}
        >
          <AppleIcon />
          Sign up with Apple
        </Button>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function RegistrationCardFooter({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  return (
    <CardFooter className={cn('justify-center', className)}>
      <p className="text-sm text-muted-foreground">
        Already have an account?{' '}
        <button
          type="button"
          className="text-primary underline-offset-4 hover:underline"
          onClick={onClick}
        >
          Sign in
        </button>
      </p>
    </CardFooter>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const RegistrationCard = {
  Provider: RegistrationCardProvider,
  Root: RegistrationCardRoot,
  Header: RegistrationCardHeader,
  EmailField: RegistrationCardEmailField,
  DisplayNameField: RegistrationCardDisplayNameField,
  PasswordField: RegistrationCardPasswordField,
  ConfirmPasswordField: RegistrationCardConfirmPasswordField,
  SubmitButton: RegistrationCardSubmitButton,
  Separator: RegistrationCardSeparator,
  SocialButtons: RegistrationCardSocialButtons,
  Footer: RegistrationCardFooter,
  Context: RegistrationContext,
};
