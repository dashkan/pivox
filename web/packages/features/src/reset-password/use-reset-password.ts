'use client';

import { useActionState, useState } from 'react';
import {
  confirmPasswordReset,
  getAuth,
  verifyPasswordResetCode,
} from 'firebase/auth';
import type {
  ResetPasswordActions,
  ResetPasswordContextValue,
  ResetPasswordState,
} from '@pivox/ui/reset-password-card';
import { firebaseErrorMessage } from '@/shared/firebase-error';

export function useResetPassword(
  oobCode: string,
  onSuccess?: () => void,
): ResetPasswordContextValue {
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');

  const [formState, formAction] = useActionState(
    async (_prev: { error: string | null; success: boolean }) => {
      if (password !== confirmPassword) {
        return { error: 'Passwords do not match', success: false };
      }
      try {
        const auth = getAuth();
        await verifyPasswordResetCode(auth, oobCode);
        await confirmPasswordReset(auth, oobCode, password);
        onSuccess?.();
        return { error: null, success: true };
      } catch (e) {
        return { error: firebaseErrorMessage(e), success: false };
      }
    },
    { error: null, success: false },
  );

  const state: ResetPasswordState = {
    password,
    confirmPassword,
    error: formState.error,
    success: formState.success,
  };

  const actions: ResetPasswordActions = {
    updatePassword: setPassword,
    updateConfirmPassword: setConfirmPassword,
    formAction,
  };

  return { state, actions };
}
