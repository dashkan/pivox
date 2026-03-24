'use client';

import { createContext, use } from 'react';
import type { LinkAccountContextValue } from './link-account-card.types';

export const LinkAccountContext = createContext<LinkAccountContextValue | null>(
  null,
);

export function useLinkAccountContext() {
  const ctx = use(LinkAccountContext);
  if (!ctx) {
    throw new Error(
      'LinkAccountCard subcomponents must be used within a LinkAccountCard.Provider',
    );
  }
  return ctx;
}
