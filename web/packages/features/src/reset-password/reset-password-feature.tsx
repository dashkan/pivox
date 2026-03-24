'use client';

import { ResetPasswordCard } from '@pivox/ui/reset-password-card';
import { useResetPassword } from './use-reset-password';

export function ResetPasswordFeature({
  oobCode,
  onSuccess,
  children,
}: {
  oobCode: string;
  onSuccess?: () => void;
  children: React.ReactNode;
}) {
  const value = useResetPassword(oobCode, onSuccess);

  return (
    <ResetPasswordCard.Provider value={value}>
      {children}
    </ResetPasswordCard.Provider>
  );
}
