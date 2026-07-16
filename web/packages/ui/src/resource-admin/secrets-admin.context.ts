'use client';

import { createContext, use } from 'react';

import type { SecretsAdminContextValue } from './types';

export const SecretsAdminContext =
  createContext<SecretsAdminContextValue | null>(null);

export function useSecretsAdmin(): SecretsAdminContextValue {
  const ctx = use(SecretsAdminContext);
  if (!ctx) {
    throw new Error(
      'SecretsAdmin subcomponents must be used within a SecretsAdmin.Provider',
    );
  }
  return ctx;
}
