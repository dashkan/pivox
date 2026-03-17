import { createFileRoute, Link, useRouter } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { applyActionCode, checkActionCode, getAuth } from 'firebase/auth';
import { Button } from '@pivox/primitives/button';
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';

type ActionSearch = {
  mode: string;
  oobCode: string;
  continueUrl?: string;
  lang?: string;
};

export const Route = createFileRoute('/auth/action')({
  validateSearch: (search: Record<string, unknown>): ActionSearch => ({
    mode: (search.mode as string) || '',
    oobCode: (search.oobCode as string) || '',
    continueUrl: (search.continueUrl as string) || undefined,
    lang: (search.lang as string) || undefined,
  }),
  component: ActionPage,
});

function ActionPage() {
  const router = useRouter();
  const { mode, oobCode } = Route.useSearch();

  // resetPassword navigates to its own page
  useEffect(() => {
    if (mode === 'resetPassword') {
      router.navigate({
        to: '/auth/reset-password',
        search: { oobCode },
      });
    }
  }, [mode, oobCode, router]);

  if (mode === 'resetPassword') {
    return (
      <ActionLayout>
        <p className="text-sm text-muted-foreground">Redirecting...</p>
      </ActionLayout>
    );
  }

  if (!oobCode) {
    return (
      <ActionLayout>
        <ActionCard
          title="Invalid link"
          description="This link is missing required parameters."
        >
          <LinkToLogin />
        </ActionCard>
      </ActionLayout>
    );
  }

  switch (mode) {
    case 'verifyEmail':
      return <VerifyEmailAction oobCode={oobCode} />;
    case 'verifyAndChangeEmail':
      return <VerifyAndChangeEmailAction oobCode={oobCode} />;
    case 'recoverEmail':
      return <RecoverEmailAction oobCode={oobCode} />;
    case 'revertSecondFactorAddition':
      return <RevertSecondFactorAction oobCode={oobCode} />;
    default:
      return (
        <ActionLayout>
          <ActionCard
            title="Unknown action"
            description="This link is not recognized."
          >
            <LinkToLogin />
          </ActionCard>
        </ActionLayout>
      );
  }
}

/* ------------------------------------------------------------------ */
/*  Action handlers                                                    */
/* ------------------------------------------------------------------ */

function VerifyEmailAction({ oobCode }: { oobCode: string }) {
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>(
    'loading',
  );
  const [message, setMessage] = useState('');

  useEffect(() => {
    const auth = getAuth();
    applyActionCode(auth, oobCode)
      .then(async () => {
        if (auth.currentUser) await auth.currentUser.reload();
        setStatus('success');
      })
      .catch((e) => {
        setStatus('error');
        setMessage(actionErrorMessage(e));
      });
  }, [oobCode]);

  if (status === 'loading') {
    return (
      <ActionLayout>
        <ActionCard title="Verifying email" description="Please wait..." />
      </ActionLayout>
    );
  }

  if (status === 'error') {
    return (
      <ActionLayout>
        <ActionCard title="Verification failed" description={message}>
          <LinkToLogin />
        </ActionCard>
      </ActionLayout>
    );
  }

  return (
    <ActionLayout>
      <ActionCard
        title="Email verified"
        description="Your email address has been verified."
      >
        <LinkToLogin label="Continue" />
      </ActionCard>
    </ActionLayout>
  );
}

function VerifyAndChangeEmailAction({ oobCode }: { oobCode: string }) {
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>(
    'loading',
  );
  const [message, setMessage] = useState('');

  useEffect(() => {
    const auth = getAuth();
    applyActionCode(auth, oobCode)
      .then(async () => {
        if (auth.currentUser) await auth.currentUser.reload();
        setStatus('success');
      })
      .catch((e) => {
        setStatus('error');
        setMessage(actionErrorMessage(e));
      });
  }, [oobCode]);

  if (status === 'loading') {
    return (
      <ActionLayout>
        <ActionCard title="Updating email" description="Please wait..." />
      </ActionLayout>
    );
  }

  if (status === 'error') {
    return (
      <ActionLayout>
        <ActionCard title="Email update failed" description={message}>
          <LinkToLogin />
        </ActionCard>
      </ActionLayout>
    );
  }

  return (
    <ActionLayout>
      <ActionCard
        title="Email address updated"
        description="Your email address has been changed successfully."
      >
        <LinkToLogin label="Continue" />
      </ActionCard>
    </ActionLayout>
  );
}

function RecoverEmailAction({ oobCode }: { oobCode: string }) {
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>(
    'loading',
  );
  const [message, setMessage] = useState('');
  const [previousEmail, setPreviousEmail] = useState('');

  useEffect(() => {
    const auth = getAuth();
    checkActionCode(auth, oobCode)
      .then((info) => {
        setPreviousEmail(info.data.email ?? '');
        return applyActionCode(auth, oobCode);
      })
      .then(() => {
        setStatus('success');
      })
      .catch((e) => {
        setStatus('error');
        setMessage(actionErrorMessage(e));
      });
  }, [oobCode]);

  if (status === 'loading') {
    return (
      <ActionLayout>
        <ActionCard title="Recovering email" description="Please wait..." />
      </ActionLayout>
    );
  }

  if (status === 'error') {
    return (
      <ActionLayout>
        <ActionCard title="Recovery failed" description={message}>
          <LinkToLogin />
        </ActionCard>
      </ActionLayout>
    );
  }

  return (
    <ActionLayout>
      <ActionCard
        title="Email recovered"
        description={`Your email has been reverted to ${previousEmail}. If you didn\u2019t request this change, consider changing your password.`}
      >
        <LinkToLogin label="Continue" />
      </ActionCard>
    </ActionLayout>
  );
}

function RevertSecondFactorAction({ oobCode }: { oobCode: string }) {
  const [status, setStatus] = useState<
    'loading' | 'confirm' | 'success' | 'error'
  >('loading');
  const [message, setMessage] = useState('');

  useEffect(() => {
    const auth = getAuth();
    checkActionCode(auth, oobCode)
      .then(() => {
        setStatus('confirm');
      })
      .catch((e) => {
        setStatus('error');
        setMessage(actionErrorMessage(e));
      });
  }, [oobCode]);

  const handleRevert = async () => {
    try {
      const auth = getAuth();
      await applyActionCode(auth, oobCode);
      setStatus('success');
    } catch (e) {
      setStatus('error');
      setMessage(actionErrorMessage(e));
    }
  };

  if (status === 'loading') {
    return (
      <ActionLayout>
        <ActionCard title="Checking link" description="Please wait..." />
      </ActionLayout>
    );
  }

  if (status === 'error') {
    return (
      <ActionLayout>
        <ActionCard title="Action failed" description={message}>
          <LinkToLogin />
        </ActionCard>
      </ActionLayout>
    );
  }

  if (status === 'confirm') {
    return (
      <ActionLayout>
        <ActionCard
          title="Remove 2-step verification?"
          description="Two-step verification was recently added to your account. If you didn't do this, you can remove it now."
        >
          <div className="flex gap-2">
            <Button size="sm" variant="destructive" onClick={handleRevert}>
              Remove 2-step verification
            </Button>
            <LinkToLogin label="Cancel" />
          </div>
        </ActionCard>
      </ActionLayout>
    );
  }

  return (
    <ActionLayout>
      <ActionCard
        title="2-step verification removed"
        description="Two-step verification has been removed from your account."
      >
        <LinkToLogin label="Continue" />
      </ActionCard>
    </ActionLayout>
  );
}

/* ------------------------------------------------------------------ */
/*  Shared components                                                  */
/* ------------------------------------------------------------------ */

function ActionLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      {children}
    </div>
  );
}

function ActionCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: React.ReactNode;
}) {
  return (
    <Card className="w-full max-w-md">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      {children && <div className="px-6 pb-6">{children}</div>}
    </Card>
  );
}

function LinkToLogin({ label = 'Back to login' }: { label?: string }) {
  return (
    <Button asChild size="sm" variant="outline">
      <Link to="/auth/login">{label}</Link>
    </Button>
  );
}

/* ------------------------------------------------------------------ */
/*  Error helper                                                       */
/* ------------------------------------------------------------------ */

function actionErrorMessage(e: unknown): string {
  if (typeof e === 'object' && e !== null && 'code' in e) {
    const code = (e as { code: string }).code;
    switch (code) {
      case 'auth/expired-action-code':
        return 'This link has expired. Please request a new one.';
      case 'auth/invalid-action-code':
        return 'This link is invalid or has already been used.';
      default:
        return 'Something went wrong. Please try again.';
    }
  }
  return 'Something went wrong. Please try again.';
}
