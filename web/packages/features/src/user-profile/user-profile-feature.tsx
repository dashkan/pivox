'use client';

import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { useUserProfile } from './use-user-profile';

export function UserProfileFeature({
  onClose,
  open,
  children,
}: {
  onClose?: () => void;
  open?: boolean;
  children: React.ReactNode;
}) {
  const value = useUserProfile(onClose, { open });

  return (
    <UserProfileCard.Provider value={value}>
      {children}
    </UserProfileCard.Provider>
  );
}
