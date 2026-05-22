'use client';

import { LoginCard } from '@pivox/ui/login-card';

import { useLogin } from './use-login';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type { User } from 'firebase/auth';

export function LoginFeature({
  transport,
  onSuccess,
  onLinkRequired,
  children,
}: {
  transport: RedirectTransport;
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: React.ReactNode;
}) {
  const value = useLogin({ transport, onSuccess, onLinkRequired });

  return <LoginCard.Provider value={value}>{children}</LoginCard.Provider>;
}
