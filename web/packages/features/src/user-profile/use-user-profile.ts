'use client';

import { useState } from 'react';
import {
  EmailAuthProvider,
  GithubAuthProvider,
  GoogleAuthProvider,
  OAuthProvider,
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
  'apple.com': { label: 'Apple', create: () => new OAuthProvider('apple.com') },
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

export function useUserProfile(onClose?: () => void): UserProfileContextValue {
  const { user, signOut, refreshUser } = useAuth();
  const [activePage, setActivePage] = useState<'account' | 'security'>(
    'account',
  );
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [totpSecret, setTotpSecret] = useState<TotpSecret | null>(null);
  const [totpStep, setTotpStep] = useState<'qr' | 'verify' | null>(null);

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
        await linkWithPopup(user, entry.create());
        // linkWithPopup updates the user object in place and triggers
        // onIdTokenChanged, so no manual refresh needed
        setSuccess(`${entry.label} account linked`);
      } catch (e) {
        setError(firebaseErrorMessage(e));
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
