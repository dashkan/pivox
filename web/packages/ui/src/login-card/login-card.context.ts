'use client';

import { createContext, use } from 'react';
import type { LoginContextValue } from './login-card.types';

export const LoginContext = createContext<LoginContextValue | null>(null);

export function useLoginContext() {
  const ctx = use(LoginContext);
  if (!ctx) {
    throw new Error(
      'LoginCard subcomponents must be used within a LoginCard.Provider',
    );
  }
  return ctx;
}
