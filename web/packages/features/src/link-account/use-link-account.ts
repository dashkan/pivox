'use client';

import { useActionState, useState } from 'react';
import {
  getAuth,
  linkWithCredential,
  signInWithEmailAndPassword,
} from 'firebase/auth';
import type {
  LinkAccountActions,
  LinkAccountContextValue,
  LinkAccountState,
} from '@pivox/ui/link-account-card';
import type { User } from 'firebase/auth';
import { firebaseErrorMessage } from '@/shared/firebase-error';
import { clearPendingLink, getPendingLink } from '@/shared/pending-link';

export function useLinkAccount(
  onSuccess?: (user: User) => void,
): LinkAccountContextValue {
  const pending = getPendingLink();
  const [password, setPassword] = useState('');

  const [formState, formAction] = useActionState(
    async (_prev: { error: string | null }) => {
      if (!pending) {
        return { error: 'No pending account to link' };
      }
      try {
        const auth = getAuth();
        const result = await signInWithEmailAndPassword(
          auth,
          pending.email,
          password,
        );
        await linkWithCredential(result.user, pending.credential);
        clearPendingLink();
        onSuccess?.(result.user);
        return { error: null };
      } catch (e) {
        return { error: firebaseErrorMessage(e) };
      }
    },
    { error: null },
  );

  const state: LinkAccountState = {
    email: pending?.email ?? '',
    providerName: pending?.providerName ?? '',
    password,
    error: formState.error,
  };

  const actions: LinkAccountActions = {
    updatePassword: setPassword,
    formAction,
  };

  return { state, actions };
}
