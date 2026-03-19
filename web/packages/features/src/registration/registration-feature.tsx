'use client';

import { RegistrationCard } from '@pivox/ui/registration-card';
import { useRegistration } from './use-registration';
import type { User } from 'firebase/auth';

export function RegistrationFeature({
  onSuccess,
  onLinkRequired,
  children,
}: {
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: React.ReactNode;
}) {
  const value = useRegistration(onSuccess, onLinkRequired);

  return (
    <RegistrationCard.Provider value={value}>
      {children}
    </RegistrationCard.Provider>
  );
}
