'use client';

import { createContext, use } from 'react';
import type { AppLayoutContextValue } from './app-layout.types';

export const AppLayoutContext = createContext<AppLayoutContextValue | null>(
  null,
);

export function useAppLayoutContext() {
  const ctx = use(AppLayoutContext);
  if (!ctx) {
    throw new Error(
      'AppLayout subcomponents must be used within an AppLayout.Provider',
    );
  }
  return ctx;
}
