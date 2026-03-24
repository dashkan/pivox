'use client';

import { LoginCard } from '@pivox/ui/login-card';
import { useLogin } from './use-login';
import type { User } from 'firebase/auth';

export function LoginFeature({
  onSuccess,
  onLinkRequired,
  children,
}: {
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: React.ReactNode;
}) {
  const value = useLogin(onSuccess, onLinkRequired);

  return <LoginCard.Provider value={value}>{children}</LoginCard.Provider>;
}
