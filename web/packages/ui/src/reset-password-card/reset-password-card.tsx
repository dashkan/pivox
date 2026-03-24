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
  ResetPasswordContext,
  useResetPasswordContext,
} from './reset-password-card.context';
import type { ResetPasswordContextValue } from './reset-password-card.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function ResetPasswordCardProvider({
  value,
  children,
}: {
  value: ResetPasswordContextValue;
  children: React.ReactNode;
}) {
  return <ResetPasswordContext value={value}>{children}</ResetPasswordContext>;
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function ResetPasswordCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useResetPasswordContext();
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

function ResetPasswordCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Set new password</CardTitle>
      <CardDescription>Enter your new password below</CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  PasswordField                                                     */
/* ------------------------------------------------------------------ */

function ResetPasswordCardPasswordField({ className }: { className?: string }) {
  const { state, actions } = useResetPasswordContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>New password</FieldLabel>
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

function ResetPasswordCardConfirmPasswordField({
  className,
}: {
  className?: string;
}) {
  const { state, actions } = useResetPasswordContext();
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

function ResetPasswordCardSubmitButton({ className }: { className?: string }) {
  const { state } = useResetPasswordContext();
  const { pending } = useFormStatus();
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Resetting…' : 'Reset password'}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  SuccessMessage                                                    */
/* ------------------------------------------------------------------ */

function ResetPasswordCardSuccessMessage({
  className,
}: {
  className?: string;
}) {
  const { state } = useResetPasswordContext();
  if (!state.success) return null;
  return (
    <div
      className={cn(
        'px-4 text-center text-sm text-muted-foreground',
        className,
      )}
    >
      Your password has been reset successfully.
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function ResetPasswordCardFooter({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  return (
    <CardFooter className={cn('justify-center', className)}>
      <button
        type="button"
        className="text-sm text-primary underline-offset-4 hover:underline"
        onClick={onClick}
      >
        Back to sign in
      </button>
    </CardFooter>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const ResetPasswordCard = {
  Provider: ResetPasswordCardProvider,
  Root: ResetPasswordCardRoot,
  Header: ResetPasswordCardHeader,
  PasswordField: ResetPasswordCardPasswordField,
  ConfirmPasswordField: ResetPasswordCardConfirmPasswordField,
  SubmitButton: ResetPasswordCardSubmitButton,
  SuccessMessage: ResetPasswordCardSuccessMessage,
  Footer: ResetPasswordCardFooter,
  Context: ResetPasswordContext,
};
