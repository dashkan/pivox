'use client';

import type { ReactNode } from 'react';
import { ClerkProvider } from '@clerk/nextjs';
import { useMantineColorScheme } from '@mantine/core';
import { clerkDarkTheme, clerkLightTheme } from '@/theme';

export function ClerkThemeProvider({ children }: { children: ReactNode }) {
  const { colorScheme } = useMantineColorScheme();

  return (
    <ClerkProvider
      appearance={{
        baseTheme: colorScheme === 'dark' ? clerkDarkTheme : clerkLightTheme,
      }}
    >
      {children}
    </ClerkProvider>
  );
}
