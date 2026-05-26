'use client';

import { AppShell } from '@pivox/ui/app-shell';

import { useAppShell } from './use-app-shell';

import type { ReactQueryApi } from '@pivox/client/react-query';
import type { AppShellContextValue, NavMainItem } from '@pivox/ui/app-shell';

/**
 * Drop-in AppShell provider — owns state + queries and yields the
 * AppShellContextValue to subcomponents. Same shape as the other
 * `*Feature` wrappers in this package (LoginFeature, RegistrationFeature,
 * etc.): a thin provider over a hook so the route writes one line.
 *
 * Explicit `AppShellContextValue` annotation on the hook return —
 * openapi-react-query's generics combined with the wrapper's
 * `ReturnType<typeof createReactQueryApi>` lose enough type fidelity
 * across the package boundary that eslint sees `error` here without
 * the annotation. useAppShell IS typed to return AppShellContextValue
 * internally; this just makes the contract explicit at the call site.
 */
export function AppShellFeature({
  $api,
  onCreateOrganization,
  navMain,
  children,
}: {
  $api: ReactQueryApi;
  onCreateOrganization: () => void;
  navMain?: NavMainItem[];
  children: React.ReactNode;
}) {
  const value: AppShellContextValue = useAppShell({
    $api,
    onCreateOrganization,
    navMain,
  });
  return <AppShell.Provider value={value}>{children}</AppShell.Provider>;
}
