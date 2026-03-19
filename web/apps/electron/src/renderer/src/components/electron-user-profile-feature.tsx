import { useEffect, useRef } from 'react';
import { getAuth } from 'firebase/auth';
import { UserProfileCard } from '@pivox/ui/user-profile-card';
import { useUserProfile } from '@pivox/features/user-profile';
import { useAuth } from '@pivox/features/auth';

const LINK_TIMEOUT_MS = 2 * 60 * 1000;

export function ElectronUserProfileFeature({
  onClose,
  open,
  children,
}: {
  onClose?: () => void;
  open?: boolean;
  children: React.ReactNode;
}) {
  const value = useUserProfile(onClose, { open });
  const { refreshUser } = useAuth();
  const linkingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Listen for deep link callbacks to refresh user after linking
  useEffect(() => {
    if (import.meta.env.DEV) return;

    const unsubscribe = window.api.onAuthDeepLink(async (data) => {
      if (data.linked === 'true') {
        if (linkingTimerRef.current) {
          clearTimeout(linkingTimerRef.current);
          linkingTimerRef.current = null;
        }
        value.actions.setLinkingProvider(null);
        await refreshUser();
      }
      if (data.error) {
        if (linkingTimerRef.current) {
          clearTimeout(linkingTimerRef.current);
          linkingTimerRef.current = null;
        }
        value.actions.setLinkingProvider(null);
      }
    });

    return unsubscribe;
  }, [refreshUser, value.actions]);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (linkingTimerRef.current) clearTimeout(linkingTimerRef.current);
    };
  }, []);

  // Override linkProvider for production builds
  const overriddenValue = import.meta.env.DEV
    ? value
    : {
        ...value,
        actions: {
          ...value.actions,
          linkProvider: async (providerId: string) => {
            try {
              const auth = getAuth();
              const user = auth.currentUser;
              if (!user) throw new Error('Not signed in');

              value.actions.setLinkingProvider(providerId);
              linkingTimerRef.current = setTimeout(() => {
                value.actions.setLinkingProvider(null);
                linkingTimerRef.current = null;
              }, LINK_TIMEOUT_MS);

              const idToken = await user.getIdToken();
              await window.api.startLinkProvider(providerId, idToken);
            } catch {
              if (linkingTimerRef.current) {
                clearTimeout(linkingTimerRef.current);
                linkingTimerRef.current = null;
              }
              value.actions.setLinkingProvider(null);
            }
          },
        },
      };

  return (
    <UserProfileCard.Provider value={overriddenValue}>
      {children}
    </UserProfileCard.Provider>
  );
}
