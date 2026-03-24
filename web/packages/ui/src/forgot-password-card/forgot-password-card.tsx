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
import {
  ForgotPasswordContext,
  useForgotPasswordContext,
} from './forgot-password-card.context';
import type { ForgotPasswordContextValue } from './forgot-password-card.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardProvider({
  value,
  children,
}: {
  value: ForgotPasswordContextValue;
  children: React.ReactNode;
}) {
  return (
    <ForgotPasswordContext value={value}>{children}</ForgotPasswordContext>
  );
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useForgotPasswordContext();
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

function ForgotPasswordCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Reset password</CardTitle>
      <CardDescription>
        Enter your email and we&apos;ll send you a reset link
      </CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  EmailField                                                        */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardEmailField({ className }: { className?: string }) {
  const { state, actions, meta } = useForgotPasswordContext();
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
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardSubmitButton({ className }: { className?: string }) {
  const { state } = useForgotPasswordContext();
  const { pending } = useFormStatus();
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Sending…' : 'Send reset link'}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  SuccessMessage                                                    */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardSuccessMessage({
  className,
}: {
  className?: string;
}) {
  const { state } = useForgotPasswordContext();
  if (!state.success) return null;
  return (
    <div
      className={cn(
        'px-4 text-center text-sm text-muted-foreground',
        className,
      )}
    >
      Check your email for a password reset link. If you don&apos;t see it,
      check your spam folder.
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function ForgotPasswordCardFooter({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  return (
    <CardFooter className={cn('justify-center', className)}>
      <p className="text-sm text-muted-foreground">
        Remember your password?{' '}
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

export const ForgotPasswordCard = {
  Provider: ForgotPasswordCardProvider,
  Root: ForgotPasswordCardRoot,
  Header: ForgotPasswordCardHeader,
  EmailField: ForgotPasswordCardEmailField,
  SubmitButton: ForgotPasswordCardSubmitButton,
  SuccessMessage: ForgotPasswordCardSuccessMessage,
  Footer: ForgotPasswordCardFooter,
  Context: ForgotPasswordContext,
};
