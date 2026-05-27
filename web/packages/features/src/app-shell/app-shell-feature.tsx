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
/**
 * Server-verified user shape used to seed the nav menu before the
 * client-side useAuth() resolves. Identical to the AuthContext user
 * subset useAppShell already projects — passing it through lets the
 * shell paint the avatar / display name / email on first SSR render
 * instead of flashing an empty state until Firebase JS hydrates.
 */
export interface InitialAppShellUser {
  displayName: string | null;
  email: string | null;
  photoURL: string | null;
}

export function AppShellFeature({
  $api,
  onCreateOrganization,
  navMain,
  initialUser,
  children,
}: {
  $api: ReactQueryApi;
  onCreateOrganization: () => void;
  navMain?: NavMainItem[];
  initialUser?: InitialAppShellUser;
  children: React.ReactNode;
}) {
  const value: AppShellContextValue = useAppShell({
    $api,
    onCreateOrganization,
    navMain,
    initialUser,
  });
  return <AppShell.Provider value={value}>{children}</AppShell.Provider>;
}
