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
 * instead of flashing an empty state until the client auth hydrates.
 */
export interface InitialAppShellUser {
  displayName: string | null;
  email: string | null;
  photoURL: string | null;
}

export function AppShellFeature({
  $api,
  onCreateOrganization,
  onOpenAccount,
  navMain,
  initialUser,
  initialActiveOrganization,
  activeOrganization,
  activeSpace,
  onSelectOrganization,
  onSelectSpace,
  children,
}: {
  $api: ReactQueryApi;
  onCreateOrganization: () => void;
  /**
   * Optional handler for the nav-user "Manage Account" action. When set, it
   * replaces the built-in profile dialog — the web BFF passes this to open the
   * Keycloak account console. When omitted (Electron), the dialog is used.
   */
  onOpenAccount?: () => void;
  navMain?: NavMainItem[];
  initialUser?: InitialAppShellUser;
  /**
   * Server-resolved active org (resource name `organizations/{slug}`)
   * read from the `pivox.active-organization` cookie during SSR. Used
   * as the initial state for the picker so SSR and client first-paint
   * render the same org — no hydration mismatch, no race with the
   * orgs-loaded validation effect overwriting the user's selection.
   * Electron (no SSR) omits this; client falls back to reading the
   * cookie directly.
   */
  initialActiveOrganization?: string | null;
  /**
   * CONTROLLED active org (resource name) derived from the route's
   * `$organization` param — the URL owns scope. Undefined leaves the
   * shell in the UNCONTROLLED cookie mode (Electron). See useAppShell.
   */
  activeOrganization?: string | null;
  /** CONTROLLED active space (resource name) or null for the org rollup. */
  activeSpace?: string | null;
  /** Select-org handler: navigate to the org's scoped home + write the cookie. */
  onSelectOrganization?: (organization: string) => void;
  /** Select-space handler (null = org rollup): navigate to the scoped route. */
  onSelectSpace?: (space: string | null) => void;
  children: React.ReactNode;
}) {
  const value: AppShellContextValue = useAppShell({
    $api,
    onCreateOrganization,
    onOpenAccount,
    navMain,
    initialUser,
    initialActiveOrganization,
    activeOrganization,
    activeSpace,
    onSelectOrganization,
    onSelectSpace,
  });
  return <AppShell.Provider value={value}>{children}</AppShell.Provider>;
}
