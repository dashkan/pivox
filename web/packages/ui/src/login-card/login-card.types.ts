import type { PivoxAuthProvider } from '../shared/auth-provider';

/**
 * Discriminates which step of the email-first flow is on screen.
 *   - 'email'    → only the email field is shown; submit resolves
 *                  SSO vs password.
 *   - 'password' → email + password; submit signs in with the
 *                  password credential.
 */
export type LoginStep = 'email' | 'password';

export interface LoginState {
  email: string;
  password: string;
  error: string | null;
  step: LoginStep;
  /**
   * Whether the user wants their email auto-filled next visit. Toggled
   * by `<LoginCard.RememberMe>`. The hook persists `state.email` to
   * localStorage on a successful sign-in when true; clears the slot
   * (unconditionally, including any previously stored value from
   * another sign-in path) when false. Defaults to true.
   *
   * This is NOT a "stay signed in" flag — Firebase Auth's default web
   * persistence (`browserLocalPersistence`) already keeps the user
   * signed in across browser restarts. "Remember me" here is purely
   * the email-autofill UX.
   */
  rememberEmail: boolean;
}

export interface LoginActions {
  updateEmail: (email: string) => void;
  updatePassword: (password: string) => void;
  setRememberEmail: (next: boolean) => void;
  /**
   * Single form action covering both steps. The feature hook branches
   * on `state.step` internally: step 1 resolves the email's SSO
   * provider (and either signs in via broker or reveals the password
   * field); step 2 runs the email/password sign-in. Same call shape
   * either way so the form's `action` doesn't need to swap.
   */
  formAction: (payload: FormData) => void;
  socialLogin: (provider: PivoxAuthProvider) => void;
}

export interface LoginMeta {
  emailRef: React.RefObject<HTMLInputElement | null>;
}

export interface LoginContextValue {
  state: LoginState;
  actions: LoginActions;
  meta: LoginMeta;
}
