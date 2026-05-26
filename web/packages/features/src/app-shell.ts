/**
 * `@pivox/features/app-shell` — provider component + hook that wire
 * the live data (orgs, spaces, user, active-org persistence,
 * sign-out) into the AppShell context interface from
 * `@pivox/ui/app-shell`. Implements the contract that the AppShell
 * compound components consume; the same UI works against any
 * provider that matches the interface (this one in production, a
 * sample constant in stories / route previews).
 */

export { AppShellFeature } from './app-shell/app-shell-feature';
export { useAppShell } from './app-shell/use-app-shell';
