export interface LinkAccountState {
  email: string;
  providerName: string;
  password: string;
  error: string | null;
}

export interface LinkAccountActions {
  updatePassword: (password: string) => void;
  formAction: (payload: FormData) => void;
}

export interface LinkAccountContextValue {
  state: LinkAccountState;
  actions: LinkAccountActions;
}
