'use client';

import { asyncHandler } from '@pivox/observability';
import {
  createUserWithEmailAndPassword,
  getAuth,
  sendEmailVerification,
  updateProfile,
} from 'firebase/auth';
import { useActionState, useRef, useState } from 'react';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type {
  RegistrationActions,
  RegistrationContextValue,
  RegistrationMeta,
  RegistrationState,
} from '@pivox/ui/registration-card';
import type { User } from 'firebase/auth';

import { BROKER_PROVIDER, signInViaBroker } from '@/shared/broker-auth';
import { firebaseErrorMessage } from '@/shared/firebase-error';


/**
 * Registration state machine. Email/password registration goes through
 * the Firebase SDK directly; social sign-up runs through the OAuth
 * broker via the injected `transport`. Social sign-up and social
 * sign-in are the same operation — `signInWithCredential` provisions
 * the account on first use — so this shares `signInViaBroker` with
 * `useLogin`.
 */
export function useRegistration(input: {
  transport: RedirectTransport;
  onSuccess?: (user: User) => void;
  onLinkRequired?: (email: string) => void;
}): RegistrationContextValue {
  const { transport, onSuccess, onLinkRequired } = input;
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
      const credential = await createUserWithEmailAndPassword(
        getAuth(),
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

    socialLogin: asyncHandler(async (provider) => {
      setError(null);
      await signInViaBroker(
        transport,
        { provider: BROKER_PROVIDER[provider] ?? provider },
        { onSuccess, onLinkRequired, setError },
      );
    }),
  };

  const meta: RegistrationMeta = { emailRef };

  return { state, actions, meta };
}
