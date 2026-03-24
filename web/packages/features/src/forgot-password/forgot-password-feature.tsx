'use client';

import { ForgotPasswordCard } from '@pivox/ui/forgot-password-card';
import { useForgotPassword } from './use-forgot-password';

export function ForgotPasswordFeature({
  children,
}: {
  children: React.ReactNode;
}) {
  const value = useForgotPassword();

  return (
    <ForgotPasswordCard.Provider value={value}>
      {children}
    </ForgotPasswordCard.Provider>
  );
}
