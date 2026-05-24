'use client';

import { createContext, use } from 'react';

import type { CreateOrgContextValue } from './create-org-card.types';

export const CreateOrgContext = createContext<CreateOrgContextValue | null>(
  null,
);

export function useCreateOrgContext() {
  const ctx = use(CreateOrgContext);
  if (!ctx) {
    throw new Error(
      'CreateOrgCard subcomponents must be used within a CreateOrgCard.Provider',
    );
  }
  return ctx;
}
