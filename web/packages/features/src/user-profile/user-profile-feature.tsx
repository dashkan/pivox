'use client';

import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { useUserProfile } from './use-user-profile';
import type { PivoxAuthProvider } from '@pivox/ui/auth';

export function UserProfileFeature({
  onClose,
  open,
  providers,
  children,
}: {
  onClose?: () => void;
  open?: boolean;
  providers?: Array<PivoxAuthProvider>;
  children: React.ReactNode;
}) {
  const value = useUserProfile(onClose, { open, providers });

  return (
    <UserProfileCard.Provider value={value}>
      {children}
    </UserProfileCard.Provider>
  );
}
