'use client';

import { createContext, use } from 'react';

import type { AppShellContextValue } from './app-shell.types';

export const AppShellContext = createContext<AppShellContextValue | null>(null);

export function useAppShellContext() {
  const ctx = use(AppShellContext);
  if (!ctx) {
    throw new Error(
      'AppShell subcomponents must be used within an AppShell.Provider',
    );
  }
  return ctx;
}
