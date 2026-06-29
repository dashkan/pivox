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
  spaces: NavSpacesSpace[];
  spacesLoading: boolean;
  navMain: NavMainItem[];
  profileOpen: boolean;
}

export interface AppShellActions {
  setActiveOrganization: (organization: string) => void;
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
