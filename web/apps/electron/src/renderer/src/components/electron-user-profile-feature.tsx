import { useAuth } from '@pivox/features/auth';
import { buildBrokerCredential } from '@pivox/features/broker';
import { useUserProfile } from '@pivox/features/user-profile';
import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { getAuth, linkWithCredential } from 'firebase/auth';

import type { PivoxAuthProvider } from '@pivox/ui/auth';
import type { ReactNode } from 'react';

// Firebase provider id -> broker provider path segment.
const BROKER_PROVIDER: Record<string, string> = {
  'google.com': 'google',
  'github.com': 'github',
};

/**
 * Electron account-linking. linkProvider runs the broker flow in the
 * system browser, then links the returned credential to the signed-in
 * user — replacing the deposit/consume custom-token bridge.
 */
export function ElectronUserProfileFeature({
  onClose,
  open,
  providers,
  children,
}: {
  onClose?: () => void;
  open?: boolean;
  providers?: Array<PivoxAuthProvider>;
  children: ReactNode;
}) {
  const value = useUserProfile(onClose, { open, providers });
  const { refreshUser } = useAuth();

  const overridden = {
    ...value,
    actions: {
      ...value.actions,
      linkProvider: async (providerId: string): Promise<void> => {
        const user = getAuth().currentUser;
        if (!user) return;
        value.actions.setLinkingProvider(providerId);
        try {
          const result = await window.api.startBrokerLogin({
            provider: BROKER_PROVIDER[providerId] ?? providerId,
          });
          if (result.ok) {
            await linkWithCredential(user, buildBrokerCredential(result));
            await refreshUser();
          }
        } catch {
          // Linking failed — surfaced by the card clearing its state.
        } finally {
          value.actions.setLinkingProvider(null);
        }
      },
    },
  };

  return (
    <UserProfileCard.Provider value={overridden}>
      {children}
    </UserProfileCard.Provider>
  );
}
