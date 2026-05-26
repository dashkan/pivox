'use client';

import { AppLayout } from '@pivox/ui/app-layout';

import { useAppLayout } from './use-app-layout';

export function AppLayoutFeature({ children }: { children: React.ReactNode }) {
  const value = useAppLayout();

  return <AppLayout.Provider value={value}>{children}</AppLayout.Provider>;
}
