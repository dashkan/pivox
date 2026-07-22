import type { NavMainItem } from './nav-main';
import type { NavSpacesSpace } from './nav-spaces';
import type { NavUserUser } from './nav-user';
import type { OrgPickerOrg } from './org-picker';

/**
 * Display + UI state for the AppShell subcomponents. Re-exports the
 * shape interfaces from each file so callers see one surface; the
 * feature hook builds this from useAuth + react-query results +
 * localStorage and hands it to AppShell.Provider.
 */
export interface AppShellState {
  user: NavUserUser | null;
  orgs: OrgPickerOrg[];
  orgsLoading: boolean;
  /** Resource name of the active org, e.g. "organizations/acme". */
  activeOrganization: string | null;
  /**
   * Resource name of the active space
   * (e.g. "organizations/acme/spaces/dev"), or null for the org
   * rollup. Derived from the route's `$space` param by the consumer;
   * the URL is the source of truth for scope.
   */
  activeSpace: string | null;
  spaces: NavSpacesSpace[];
  spacesLoading: boolean;
  navMain: NavMainItem[];
  profileOpen: boolean;
}

export interface AppShellActions {
  /**
   * Select an organization. In the web app the consumer wires this to
   * navigate to the org's scoped home (the URL owns scope) and write
   * the last-visited cookie; Electron falls back to cookie-only state.
   */
  setActiveOrganization: (organization: string) => void;
  /**
   * Select a space within the active org, or null for the org rollup
   * ("All spaces"). The consumer wires this to navigate to the scoped
   * route. No-op when the shell isn't scope-routed (Electron).
   */
  selectSpace: (space: string | null) => void;
  /**
   * Navigate to the create-organization flow. Concrete implementation
   * lives in the route layer (`router.navigate({ to: '/auth/create-org' })`).
   */
  createOrganization: () => void;
  /**
   * Open account management. When provided (web BFF → Keycloak account
   * console), nav-user calls this for "Manage Account". When omitted
   * (Electron), nav-user falls back to opening the in-app profile dialog via
   * `setProfileOpen`.
   */
  openAccount?: () => void;
  setProfileOpen: (open: boolean) => void;
  signOut: () => void | Promise<void>;
}

export interface AppShellContextValue {
  state: AppShellState;
  actions: AppShellActions;
}
