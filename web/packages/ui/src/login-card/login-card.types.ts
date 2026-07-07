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
   * This is NOT a "stay signed in" flag — the OIDC session's default
   * persistence already keeps the user signed in across browser
   * restarts. "Remember me" here is purely the email-autofill UX.
   */
  rememberEmail: boolean;
  /**
   * True while a social or SSO broker flow is in flight — the popup /
   * OS browser is open and we're waiting on the credential. UI uses
   * this to disable form inputs and swap the submit button to "Cancel
   * sign-in" so the user has an explicit way out without hunting for
   * the popup window.
   *
   * Not used for the password sign-in path; that's already covered by
   * the form's `useFormStatus().pending`.
   */
  brokerInFlight: boolean;
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
  /**
   * Cancels any in-flight broker flow. The transport tears down its
   * popup / loopback server / scheme registration; the broker
   * promise settles as user-cancelled and `brokerInFlight` flips back
   * to false. No-op if nothing is in flight.
   */
  cancelBrokerFlow: () => void;
}

export interface LoginMeta {
  emailRef: React.RefObject<HTMLInputElement | null>;
}

export interface LoginContextValue {
  state: LoginState;
  actions: LoginActions;
  meta: LoginMeta;
}
