'use client';

import { LoginCard } from '@pivox/ui/login-card';

import { useLogin } from './use-login';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type { LoginStep } from '@pivox/ui/login-card';
import type { User } from 'firebase/auth';

export function LoginFeature({
  transport,
  step,
  onStepChange,
  onSuccess,
  onLinkRequired,
  children,
}: {
  transport: RedirectTransport;
  /** Current step. Lifted to the URL by the route so back-nav works. */
  step: LoginStep;
  /**
   * Step transition. Pass `{ replace: true }` for auto-corrections
   * (rollback on email edit, refresh-fallback) so the synthetic
   * history entry doesn't survive in the back stack.
   */
  onStepChange: (step: LoginStep, opts?: { replace?: boolean }) => void;
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: React.ReactNode;
}) {
  const value = useLogin({
    transport,
    step,
    onStepChange,
    onSuccess,
    onLinkRequired,
  });

  return <LoginCard.Provider value={value}>{children}</LoginCard.Provider>;
}
