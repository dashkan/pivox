import type { PivoxAuthProvider } from '../shared/auth-provider';

export interface LoginState {
  email: string;
  password: string;
  error: string | null;
}

export interface LoginActions {
  updateEmail: (email: string) => void;
  updatePassword: (password: string) => void;
  formAction: (payload: FormData) => void;
  socialLogin: (provider: PivoxAuthProvider) => void;
  ssoLogin: () => void;
}

export interface LoginMeta {
  emailRef: React.RefObject<HTMLInputElement | null>;
}

export interface LoginContextValue {
  state: LoginState;
  actions: LoginActions;
  meta: LoginMeta;
}
