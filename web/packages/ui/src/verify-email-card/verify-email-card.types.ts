export interface VerifyEmailState {
  email: string | null;
  resent: boolean;
  error: string | null;
}

export interface VerifyEmailActions {
  resendVerification: () => void;
}

export interface VerifyEmailContextValue {
  state: VerifyEmailState;
  actions: VerifyEmailActions;
}
