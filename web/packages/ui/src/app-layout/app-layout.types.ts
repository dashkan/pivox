export interface AppLayoutState {
  user: {
    displayName: string | null;
    email: string | null;
    photoURL: string | null;
  } | null;
  loading: boolean;
  profileOpen: boolean;
}

export interface AppLayoutActions {
  setProfileOpen: (open: boolean) => void;
  signOut: () => Promise<void>;
  navigateToLogin: () => void;
}

export interface AppLayoutContextValue {
  state: AppLayoutState;
  actions: AppLayoutActions;
}
