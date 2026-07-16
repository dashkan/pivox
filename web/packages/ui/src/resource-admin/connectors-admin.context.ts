'use client';

import { createContext, use } from 'react';

import type { ConnectorsAdminContextValue } from './types';

export const ConnectorsAdminContext =
  createContext<ConnectorsAdminContextValue | null>(null);

export function useConnectorsAdmin(): ConnectorsAdminContextValue {
  const ctx = use(ConnectorsAdminContext);
  if (!ctx) {
    throw new Error(
      'ConnectorsAdmin subcomponents must be used within a ConnectorsAdmin.Provider',
    );
  }
  return ctx;
}
