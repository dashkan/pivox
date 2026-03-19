'use client';

import { useActionState, useRef, useState } from 'react';
import {
  GithubAuthProvider,
  GoogleAuthProvider,
  OAuthProvider,
  createUserWithEmailAndPassword,
  getAuth,
  sendEmailVerification,
  signInWithPopup,
  updateProfile,
} from 'firebase/auth';
import type {
  RegistrationActions,
  RegistrationContextValue,
  RegistrationMeta,
  RegistrationState,
} from '@pivox/ui/registration-card';
import type { User } from 'firebase/auth';
import type { FirebaseError } from 'firebase/app';
import { firebaseErrorMessage } from '@/shared/firebase-error';
import { setPendingLink } from '@/shared/pending-link';

const socialProviders = {
  google: () => new GoogleAuthProvider(),
  github: () => new GithubAuthProvider(),
  apple: () => new OAuthProvider('apple.com'),
} as const;

const providerNames: Record<string, string> = {
  google: 'Google',
  github: 'GitHub',
  apple: 'Apple',
};

export function useRegistration(
  onSuccess?: (user: User) => void,
  onLinkRequired?: (email: string) => void,
): RegistrationContextValue {
  const emailRef = useRef<HTMLInputElement | null>(null);
  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const [, formAction] = useActionState(async () => {
    setError(null);
    if (password !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }
    if (!displayName.trim()) {
      setError('Display name is required');
      return;
    }
    try {
      const auth = getAuth();
      const credential = await createUserWithEmailAndPassword(
        auth,
        email,
        password,
      );
      await updateProfile(credential.user, { displayName: displayName.trim() });
      await sendEmailVerification(credential.user);
      onSuccess?.(credential.user);
    } catch (e) {
      setError(firebaseErrorMessage(e));
    }
  }, null);

  const state: RegistrationState = {
    email,
    displayName,
    password,
    confirmPassword,
    error,
  };

  const actions: RegistrationActions = {
    updateEmail: setEmail,
    updateDisplayName: setDisplayName,
    updatePassword: setPassword,
    updateConfirmPassword: setConfirmPassword,
    formAction,

    socialLogin: async (provider) => {
      setError(null);
      try {
        const auth = getAuth();
        const result = await signInWithPopup(auth, socialProviders[provider]());
        onSuccess?.(result.user);
      } catch (e) {
        const err = e as FirebaseError;
        if (
          err.code === 'auth/account-exists-with-different-credential' &&
          err.customData?.email
        ) {
          const credential = OAuthProvider.credentialFromError(err);
          if (credential) {
            setPendingLink({
              email: err.customData.email as string,
              credential,
              providerName: providerNames[provider] ?? provider,
            });
            onLinkRequired?.(err.customData.email as string);
            return;
          }
        }
        if (err.code !== 'auth/popup-closed-by-user') {
          setError(firebaseErrorMessage(e));
        }
      }
    },
  };

  const meta: RegistrationMeta = { emailRef };

  return { state, actions, meta };
}
