'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  EmailAuthProvider,
  GithubAuthProvider,
  GoogleAuthProvider,
  TotpMultiFactorGenerator,
  deleteUser,
  linkWithCredential,
  linkWithPopup,
  multiFactor,
  reauthenticateWithCredential,
  reauthenticateWithPopup,
  sendEmailVerification,
  unlink,
  updatePassword,
  updateProfile,
  verifyBeforeUpdateEmail,
} from 'firebase/auth';
import type { AuthProvider, TotpSecret, User } from 'firebase/auth';
import type {
  UserProfileActions,
  UserProfileContextValue,
  UserProfileState,
} from '@pivox/ui/user-profile-card';
import { useAuth } from '@/auth/use-auth';
import { firebaseErrorMessage } from '@/shared/firebase-error';

const oauthProviders: Record<
  string,
  { label: string; create: () => AuthProvider }
> = {
  'google.com': {
    label: 'Google',
    create: () => {
      const p = new GoogleAuthProvider();
      p.setCustomParameters({ prompt: 'select_account' });
      return p;
    },
  },
  'github.com': { label: 'GitHub', create: () => new GithubAuthProvider() },
};

async function reauthenticate(user: User): Promise<void> {
  // Try OAuth providers first (Google, etc.)
  for (const provider of user.providerData) {
    const entry = oauthProviders[provider.providerId];
    if (entry) {
      await reauthenticateWithPopup(user, entry.create());
      return;
    }
  }
  // No supported provider found — caller should handle the error
  throw Object.assign(new Error('Reauthentication required'), {
    code: 'auth/requires-recent-login',
  });
}

export function useUserProfile(
  onClose?: () => void,
  options?: { open?: boolean },
): UserProfileContextValue {
  const { user, signOut, refreshUser } = useAuth();

  // Refresh user data each time the profile opens to pick up cross-session
  // changes (e.g., provider unlinked on another device). Runs in the feature
  // layer so both web and Electron benefit without duplicating the logic.
  const open = options?.open;
  useEffect(() => {
    if (open) {
      refreshUser();
    }
  }, [open, refreshUser]);

  const [activePage, setActivePage] = useState<'account' | 'security'>(
    'account',
  );
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [totpSecret, setTotpSecret] = useState<TotpSecret | null>(null);
  const [totpStep, setTotpStep] = useState<'qr' | 'verify' | null>(null);
  const [linkingProvider, setLinkingProvider] = useState<string | null>(null);
  const linkingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // How long the user has to complete a link flow before it times out.
  const LINK_TIMEOUT_MS = 2 * 60 * 1000;

  const clearLinking = useCallback(() => {
    if (linkingTimerRef.current) {
      clearTimeout(linkingTimerRef.current);
      linkingTimerRef.current = null;
    }
    setLinkingProvider(null);
  }, []);

  // Clean up timer on unmount.
  useEffect(() => {
    return () => {
      if (linkingTimerRef.current) clearTimeout(linkingTimerRef.current);
    };
  }, []);

  const clearStatus = () => {
    setError(null);
    setSuccess(null);
  };

  let mfaEnrolled = false;
  try {
    if (user) {
      mfaEnrolled = multiFactor(user).enrolledFactors.some(
        (f) => f.factorId === TotpMultiFactorGenerator.FACTOR_ID,
      );
    }
  } catch {
    // user object may be stale during refresh
  }

  const linkedProviderIds = new Set(
    user?.providerData.map((p) => p.providerId) ?? [],
  );
  const availableProviders = Object.entries(oauthProviders)
    .filter(([id]) => !linkedProviderIds.has(id))
    .map(([id, entry]) => ({ providerId: id, label: entry.label }));

  const state: UserProfileState = {
    displayName: user?.displayName ?? null,
    email: user?.email ?? null,
    photoURL: user?.photoURL || null,
    emailVerified: user?.emailVerified ?? false,
    providers:
      user?.providerData.map((p) => ({
        providerId: p.providerId,
        displayName: p.displayName,
        email: p.email,
        photoURL: p.photoURL,
      })) ?? [],
    availableProviders,
    activePage,
    error,
    success,
    mfaEnrolled,
    totpEnrollment:
      totpStep && totpSecret
        ? {
            step: totpStep,
            qrUrl: totpSecret.generateQrCodeUrl(user?.email ?? '', 'Pivox'),
            secret: totpSecret.secretKey,
          }
        : null,
    linkingProvider,
  };

  const actions: UserProfileActions = {
    setActivePage,
    clearStatus,
    updateDisplayName: async (name) => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        await updateProfile(user, { displayName: name });
        setSuccess('Display name updated');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    updatePhoto: async (_file) => {
      clearStatus();
      // TODO: Upload file to Firebase Storage, get download URL,
      //       then call updateProfile(user, { photoURL })
      setError('Photo upload is not yet implemented');
    },

    setPhotoURL: async (url) => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        await updateProfile(user, { photoURL: url });
        await refreshUser();
        setSuccess('Profile photo updated');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    removePhoto: async () => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        await updateProfile(user, { photoURL: '' });
        await refreshUser();
        setSuccess('Photo removed');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    setPassword: async (newPassword) => {
      clearStatus();
      try {
        if (!user || !user.email) throw new Error('Not signed in');
        const credential = EmailAuthProvider.credential(
          user.email,
          newPassword,
        );
        await linkWithCredential(user, credential);
        await sendEmailVerification(user);
        setSuccess('Password set. Check your email to verify your account.');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    sendVerificationEmail: async () => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        await sendEmailVerification(user);
        setSuccess('Verification email sent. Check your inbox.');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    changePassword: async (currentPassword, newPassword) => {
      clearStatus();
      try {
        if (!user || !user.email) throw new Error('Not signed in');
        const credential = EmailAuthProvider.credential(
          user.email,
          currentPassword,
        );
        await reauthenticateWithCredential(user, credential);
        await updatePassword(user, newPassword);
        setSuccess('Password updated');
      } catch (e) {
        setError(firebaseErrorMessage(e));
        throw e;
      }
    },

    changeEmail: async (newEmail) => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');

        const update = () => verifyBeforeUpdateEmail(user, newEmail);
        try {
          await update();
        } catch (e) {
          if (
            typeof e === 'object' &&
            e !== null &&
            'code' in e &&
            (e as { code: string }).code === 'auth/requires-recent-login'
          ) {
            await reauthenticate(user);
            await update();
          } else {
            throw e;
          }
        }

        setSuccess(
          `Verification email sent to ${newEmail}. Click the link to confirm.`,
        );
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    startTotpEnrollment: async () => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        if (!user.emailVerified) {
          setError(
            'You must verify your email before enabling two-factor authentication.',
          );
          return;
        }

        const enroll = async () => {
          const session = await multiFactor(user).getSession();
          const secret = await TotpMultiFactorGenerator.generateSecret(session);
          setTotpSecret(secret);
          setTotpStep('qr');
        };

        try {
          await enroll();
        } catch (e) {
          if (
            typeof e === 'object' &&
            e !== null &&
            'code' in e &&
            (e as { code: string }).code === 'auth/requires-recent-login'
          ) {
            await reauthenticate(user);
            await enroll();
          } else {
            throw e;
          }
        }
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    verifyTotpEnrollment: async (code) => {
      clearStatus();
      try {
        if (!user || !totpSecret) throw new Error('No enrollment in progress');
        const assertion = TotpMultiFactorGenerator.assertionForEnrollment(
          totpSecret,
          code,
        );
        await multiFactor(user).enroll(assertion, 'Authenticator app');
        setTotpSecret(null);
        setTotpStep(null);
        await refreshUser();
        setSuccess('Two-factor authentication enabled');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    cancelTotpEnrollment: () => {
      setTotpSecret(null);
      setTotpStep(null);
    },

    unenrollTotp: async () => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        const factor = multiFactor(user).enrolledFactors.find(
          (f) => f.factorId === TotpMultiFactorGenerator.FACTOR_ID,
        );
        if (!factor) throw new Error('No TOTP factor enrolled');

        const unenroll = () => multiFactor(user).unenroll(factor);
        try {
          await unenroll();
        } catch (e) {
          if (
            typeof e === 'object' &&
            e !== null &&
            'code' in e &&
            (e as { code: string }).code === 'auth/requires-recent-login'
          ) {
            await reauthenticate(user);
            await unenroll();
          } else {
            throw e;
          }
        }

        await refreshUser();
        setSuccess('Two-factor authentication disabled');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    linkProvider: async (providerId) => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        const entry = oauthProviders[providerId];
        if (!entry) throw new Error('Unsupported provider');

        setLinkingProvider(providerId);
        linkingTimerRef.current = setTimeout(() => {
          setLinkingProvider(null);
          linkingTimerRef.current = null;
          setError(`Linking ${entry.label} timed out. Please try again.`);
        }, LINK_TIMEOUT_MS);

        await linkWithPopup(user, entry.create());
        clearLinking();
        setSuccess(`${entry.label} account linked`);
      } catch (e) {
        clearLinking();
        setError(firebaseErrorMessage(e));
      }
    },

    setLinkingProvider: (providerId: string | null) => {
      if (providerId) {
        setLinkingProvider(providerId);
      } else {
        clearLinking();
      }
    },

    unlinkProvider: async (providerId) => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');
        await unlink(user, providerId);
        // unlink updates the user object in place and triggers
        // onIdTokenChanged, so no manual refresh needed
        setSuccess('Provider unlinked');
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    deleteAccount: async () => {
      clearStatus();
      try {
        if (!user) throw new Error('Not signed in');

        const remove = () => deleteUser(user);
        try {
          await remove();
        } catch (e) {
          if (
            typeof e === 'object' &&
            e !== null &&
            'code' in e &&
            (e as { code: string }).code === 'auth/requires-recent-login'
          ) {
            await reauthenticate(user);
            await remove();
          } else {
            throw e;
          }
        }

        onClose?.();
      } catch (e) {
        setError(firebaseErrorMessage(e));
      }
    },

    signOut: async () => {
      onClose?.();
      await signOut();
    },
  };

  return { state, actions };
}
