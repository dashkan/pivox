'use client';

import { useState } from 'react';
import type { AppLayoutContextValue } from '@pivox/ui/app-layout';
import { useAuth } from '@/auth/use-auth';

export function useAppLayout(
  onNavigateToLogin: () => void,
): AppLayoutContextValue {
  const { user, loading, signOut } = useAuth();
  const [profileOpen, setProfileOpen] = useState(false);

  return {
    state: {
      user: user
        ? {
            displayName: user.displayName,
            email: user.email,
            photoURL: user.photoURL,
          }
        : null,
      loading,
      profileOpen,
    },
    actions: {
      setProfileOpen,
      signOut: async () => {
        setProfileOpen(false);
        await signOut();
      },
      navigateToLogin: onNavigateToLogin,
    },
  };
}
