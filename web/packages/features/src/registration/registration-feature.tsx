'use client';

import { RegistrationCard } from '@pivox/ui/registration-card';

import { useRegistration } from './use-registration';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type { User } from 'firebase/auth';

export function RegistrationFeature({
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
  const value = useRegistration({ transport, onSuccess, onLinkRequired });

  return (
    <RegistrationCard.Provider value={value}>
      {children}
    </RegistrationCard.Provider>
  );
}
