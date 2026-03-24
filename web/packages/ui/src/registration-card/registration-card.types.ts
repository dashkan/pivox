import type { PivoxAuthProvider } from '../shared/auth-provider';

export interface RegistrationState {
  email: string;
  displayName: string;
  password: string;
  confirmPassword: string;
  error: string | null;
}

export interface RegistrationActions {
  updateEmail: (email: string) => void;
  updateDisplayName: (name: string) => void;
  updatePassword: (password: string) => void;
  updateConfirmPassword: (password: string) => void;
  formAction: (payload: FormData) => void;
  socialLogin: (provider: PivoxAuthProvider) => void;
}

export interface RegistrationMeta {
  emailRef: React.RefObject<HTMLInputElement | null>;
}

export interface RegistrationContextValue {
  state: RegistrationState;
  actions: RegistrationActions;
  meta: RegistrationMeta;
}
