'use client';

import { createContext, use } from 'react';
import type { ForgotPasswordContextValue } from './forgot-password-card.types';

export const ForgotPasswordContext =
  createContext<ForgotPasswordContextValue | null>(null);

export function useForgotPasswordContext() {
  const ctx = use(ForgotPasswordContext);
  if (!ctx) {
    throw new Error(
      'ForgotPasswordCard subcomponents must be used within a ForgotPasswordCard.Provider',
    );
  }
  return ctx;
}
