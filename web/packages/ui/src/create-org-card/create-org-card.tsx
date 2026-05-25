'use client';

import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { Field, FieldError, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { cn } from '@pivox/primitives/utils';
import { useFormStatus } from 'react-dom';

import {
  CreateOrgContext,
  useCreateOrgContext,
} from './create-org-card.context';

import type { CreateOrgContextValue } from './create-org-card.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function CreateOrgCardProvider({
  value,
  children,
}: {
  value: CreateOrgContextValue;
  children: React.ReactNode;
}) {
  return <CreateOrgContext value={value}>{children}</CreateOrgContext>;
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function CreateOrgCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useCreateOrgContext();
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

function CreateOrgCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Create your organization</CardTitle>
      <CardDescription>
        Set up your first Pivox workspace to continue.
      </CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  DisplayNameField                                                  */
/* ------------------------------------------------------------------ */

function CreateOrgCardDisplayNameField({ className }: { className?: string }) {
  const { state, actions, meta } = useCreateOrgContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Organization name</FieldLabel>
      <Input
        // eslint-disable-next-line react-hooks/refs -- forwarding the ref object to a JSX `ref={}` prop, not reading `.current` during render
        ref={meta.displayNameRef}
        name="displayName"
        type="text"
        autoComplete="organization"
        value={state.displayName}
        onChange={(e) => {
          actions.updateDisplayName(e.target.value);
        }}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  ShortNameField                                                    */
/* ------------------------------------------------------------------ */

function CreateOrgCardShortNameField({ className }: { className?: string }) {
  const { state, actions } = useCreateOrgContext();
  const { pending } = useFormStatus();
  return (
    <Field className={cn('px-4', className)}>
      <FieldLabel>Short name</FieldLabel>
      <Input
        name="organizationId"
        type="text"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        value={state.organizationId}
        onChange={(e) => {
          actions.updateOrganizationId(e.target.value);
        }}
        disabled={pending}
      />
    </Field>
  );
}

/* ------------------------------------------------------------------ */
/*  SlugHint                                                          */
/* ------------------------------------------------------------------ */

function CreateOrgCardSlugHint({ className }: { className?: string }) {
  return (
    <p className={cn('px-4 text-xs text-muted-foreground', className)}>
      Permanent. 4–20 characters · lowercase letters, numbers, hyphens · must
      start with a letter.
    </p>
  );
}

/* ------------------------------------------------------------------ */
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function CreateOrgCardSubmitButton({ className }: { className?: string }) {
  const { state } = useCreateOrgContext();
  const { pending } = useFormStatus();
  // Server-side buf-validate enforces ^[a-z][a-z0-9-]{3,19}$. Mirroring
  // here keeps the button disabled on a guaranteed-InvalidArgument
  // input — saves a roundtrip and gives faster feedback.
  const slugValid = /^[a-z][a-z0-9-]{3,19}$/.test(state.organizationId);
  const nameValid = state.displayName.trim().length > 0;
  const disabled = pending || !slugValid || !nameValid;
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={disabled}>
        {pending ? 'Creating…' : 'Create organization'}
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function CreateOrgCardFooter({ className }: { className?: string }) {
  const { actions } = useCreateOrgContext();
  return (
    <CardFooter className={cn('justify-center', className)}>
      <p className="text-sm text-muted-foreground">
        Wrong account?{' '}
        <button
          type="button"
          className="text-primary underline-offset-4 hover:underline"
          onClick={actions.signOut}
        >
          Sign out
        </button>
      </p>
    </CardFooter>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const CreateOrgCard = {
  Provider: CreateOrgCardProvider,
  Root: CreateOrgCardRoot,
  Header: CreateOrgCardHeader,
  DisplayNameField: CreateOrgCardDisplayNameField,
  ShortNameField: CreateOrgCardShortNameField,
  SlugHint: CreateOrgCardSlugHint,
  SubmitButton: CreateOrgCardSubmitButton,
  Footer: CreateOrgCardFooter,
  Context: CreateOrgContext,
};
