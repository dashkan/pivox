export interface ResetPasswordState {
  password: string;
  confirmPassword: string;
  error: string | null;
  success: boolean;
}

export interface ResetPasswordActions {
  updatePassword: (password: string) => void;
  updateConfirmPassword: (password: string) => void;
  formAction: (payload: FormData) => void;
}

export interface ResetPasswordContextValue {
  state: ResetPasswordState;
  actions: ResetPasswordActions;
}
