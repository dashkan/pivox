'use client';

import { useActionState, useRef, useState } from 'react';
import { getAuth, sendPasswordResetEmail } from 'firebase/auth';
import type {
  ForgotPasswordActions,
  ForgotPasswordContextValue,
  ForgotPasswordMeta,
  ForgotPasswordState,
} from '@pivox/ui/forgot-password-card';
import { firebaseErrorMessage } from '@/shared/firebase-error';

export function useForgotPassword(): ForgotPasswordContextValue {
  const emailRef = useRef<HTMLInputElement | null>(null);
  const [email, setEmail] = useState('');

  const [formState, formAction] = useActionState(
    async (_prev: { error: string | null; success: boolean }) => {
      try {
        const auth = getAuth();
        await sendPasswordResetEmail(auth, email);
        return { error: null, success: true };
      } catch (e) {
        return { error: firebaseErrorMessage(e), success: false };
      }
    },
    { error: null, success: false },
  );

  const state: ForgotPasswordState = {
    email,
    error: formState.error,
    success: formState.success,
  };

  const actions: ForgotPasswordActions = {
    updateEmail: setEmail,
    formAction,
  };

  const meta: ForgotPasswordMeta = { emailRef };

  return { state, actions, meta };
}
