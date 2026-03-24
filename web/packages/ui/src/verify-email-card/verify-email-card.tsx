'use client';

import { cn } from '@pivox/primitives/utils';
import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { FieldError } from '@pivox/primitives/field';
import {
  VerifyEmailContext,
  useVerifyEmailContext,
} from './verify-email-card.context';
import type { VerifyEmailContextValue } from './verify-email-card.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function VerifyEmailCardProvider({
  value,
  children,
}: {
  value: VerifyEmailContextValue;
  children: React.ReactNode;
}) {
  return <VerifyEmailContext value={value}>{children}</VerifyEmailContext>;
}

/* ------------------------------------------------------------------ */
/*  Root                                                              */
/* ------------------------------------------------------------------ */

function VerifyEmailCardRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        'flex min-h-screen items-center justify-center p-4',
        className,
      )}
    >
      <Card className="w-full max-w-sm">
        <div className="flex flex-col gap-4">{children}</div>
      </Card>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Header                                                            */
/* ------------------------------------------------------------------ */

function VerifyEmailCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn('text-center', className)}>
      <CardTitle className="text-xl">Check your email</CardTitle>
      <CardDescription>We sent you a verification link</CardDescription>
    </CardHeader>
  );
}

/* ------------------------------------------------------------------ */
/*  Message                                                           */
/* ------------------------------------------------------------------ */

function VerifyEmailCardMessage({ className }: { className?: string }) {
  const { state } = useVerifyEmailContext();
  return (
    <div
      className={cn(
        'px-4 text-center text-sm text-muted-foreground',
        className,
      )}
    >
      {state.email ? (
        <>
          We sent a verification email to{' '}
          <span className="font-medium text-foreground">{state.email}</span>.
          Click the link in the email to verify your account.
        </>
      ) : (
        'Click the link in the email to verify your account.'
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  ResendButton                                                      */
/* ------------------------------------------------------------------ */

function VerifyEmailCardResendButton({ className }: { className?: string }) {
  const { state, actions } = useVerifyEmailContext();
  return (
    <div className={cn('flex flex-col gap-4 px-4', className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      {state.resent && (
        <p className="text-center text-sm text-muted-foreground">
          Verification email resent.
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        className="w-full"
        onClick={actions.resendVerification}
      >
        Resend verification email
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function VerifyEmailCardFooter({
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

export const VerifyEmailCard = {
  Provider: VerifyEmailCardProvider,
  Root: VerifyEmailCardRoot,
  Header: VerifyEmailCardHeader,
  Message: VerifyEmailCardMessage,
  ResendButton: VerifyEmailCardResendButton,
  Footer: VerifyEmailCardFooter,
  Context: VerifyEmailContext,
};
