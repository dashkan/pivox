import { useEffect, useState } from 'react';
import { getAuth, signInWithCustomToken } from 'firebase/auth';
import { LoginCard } from '@pivox/ui/login-card';
import { useLogin } from '@pivox/features/login';
import type { ReactNode } from 'react';
import type { User } from 'firebase/auth';
import type { PivoxAuthProvider } from '@pivox/ui/auth';

export function ElectronLoginFeature({
  onSuccess,
  onLinkRequired,
  children,
}: {
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
  children: ReactNode;
}) {
  const value = useLogin(onSuccess, onLinkRequired);
  const [deepLinkError, setDeepLinkError] = useState<string | null>(null);

  // Listen for deep link callbacks from the main process
  useEffect(() => {
    // In dev mode, popup auth works — no deep link listener needed
    if (import.meta.env.DEV) return;

    return window.api.onAuthDeepLink(async (data) => {
      if (data.error) {
        setDeepLinkError(data.error);
        return;
      }

      if (data.token) {
        try {
          const auth = getAuth();
          const credential = await signInWithCustomToken(auth, data.token);
          onSuccess?.(credential.user);
        } catch (e) {
          const msg = e instanceof Error ? e.message : String(e);
          console.error('signInWithCustomToken failed:', msg);
          setDeepLinkError(`Sign-in failed: ${msg}`);
        }
      }
    });
  }, [onSuccess]);

  // Override socialLogin for production builds
  const overriddenValue = import.meta.env.DEV
    ? value
    : {
        ...value,
        state: {
          ...value.state,
          error: deepLinkError || value.state.error,
        },
        actions: {
          ...value.actions,
          socialLogin: async (provider: PivoxAuthProvider) => {
            setDeepLinkError(null);
            await window.api.startSocialLogin(provider);
          },
        },
      };

  return (
    <LoginCard.Provider value={overriddenValue}>{children}</LoginCard.Provider>
  );
}
