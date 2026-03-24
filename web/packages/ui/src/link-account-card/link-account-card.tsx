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
  LinkAccountContext,
  useLinkAccountContext,
} from './link-account-card.context';
import type { LinkAccountContextValue } from './link-account-card.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function LinkAccountCardProvider({
  value,
  children,
}: {
  value: LinkAccountContextValue;
  children: React.ReactNode;
}) {
  return <LinkAccountContext value={value}>{children}</LinkAccountContext>;
}

/* ------------------------------------------------------------------ */
/*  Root                                                              */
/* ------------------------------------------------------------------ */

function LinkAccountCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useLinkAccountContext();
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

function LinkAccountCardHeader({ className }: { className?: string }) {
  const { state } = useLinkAccountContext();
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Link your account</CardTitle>
      <CardDescription>
        An account with{' '}
        <span className="font-medium text-foreground">{state.email}</span>{' '}
        already exists. Enter your password to link your {state.providerName}{' '}
        account.
      </CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  PasswordField                                                     */
/* ------------------------------------------------------------------ */

function LinkAccountCardPasswordField({ className }: { className?: string }) {
  const { state, actions } = useLinkAccountContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Password</FieldLabel>
      <Input
        name="password"
        type="password"
        autoComplete="current-password"
        value={state.password}
        onChange={(e) => actions.updatePassword(e.target.value)}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function LinkAccountCardSubmitButton({ className }: { className?: string }) {
  const { state } = useLinkAccountContext();
  const { pending } = useFormStatus();
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Linking…' : 'Sign in & link account'}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function LinkAccountCardFooter({
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

export const LinkAccountCard = {
  Provider: LinkAccountCardProvider,
  Root: LinkAccountCardRoot,
  Header: LinkAccountCardHeader,
  PasswordField: LinkAccountCardPasswordField,
  SubmitButton: LinkAccountCardSubmitButton,
  Footer: LinkAccountCardFooter,
  Context: LinkAccountContext,
};
