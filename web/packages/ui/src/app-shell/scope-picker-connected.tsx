'use client';

import { useAppShellContext } from './app-shell.context';
import { AppShellScopePicker } from './scope-picker';

/**
 * Context-bound adapter for the pure {@link AppShellScopePicker}. Reads
 * orgs / active scope from AppShellContext and forwards the select
 * actions the provider injected (navigation + last-visited cookie in
 * the web app; cookie-only state in Electron). Keeps the prop-driven
 * picker router-agnostic and unit-testable while the sidebar stays a
 * zero-config compound.
 */
export function AppShellScopePickerConnected() {
  const { state, actions } = useAppShellContext();
  return (
    <AppShellScopePicker
      orgs={state.orgs}
      activeOrganization={state.activeOrganization}
      spaces={state.spaces}
      activeSpace={state.activeSpace}
      orgsLoading={state.orgsLoading}
      onSelectOrganization={actions.setActiveOrganization}
      onSelectSpace={actions.selectSpace}
    />
  );
}
