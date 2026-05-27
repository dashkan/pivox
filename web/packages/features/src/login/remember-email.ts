/**
 * localStorage policy for the login auto-fill email slot.
 *
 * Extracted from `use-login.ts` so the decision logic is testable in
 * isolation — the hook's React state machinery doesn't need to be in
 * the test loop. Pure function except for the localStorage side
 * effect.
 *
 * Policy summary (see tests/remember-email.test.ts for behavioral
 * spec):
 *   - Password sign-in: respect the remember-me checkbox. ON stores
 *     the email, OFF clears it (covers the user explicitly opting
 *     out — wipes any value left from a prior session).
 *   - Social or SSO sign-in: always clear, regardless of the
 *     checkbox. The auto-fill field isn't useful for those flows
 *     (social uses provider buttons, SSO re-enters the email each
 *     time to discover the provider), and a stale value from an
 *     earlier password sign-in would confuse subsequent visits.
 */

export const LAST_EMAIL_STORAGE_KEY = 'pivox.login.last-email';

export type SignInMethod = 'password' | 'social' | 'sso';

export interface ApplyRememberMeInput {
  /** Which sign-in path completed. */
  method: SignInMethod;
  /** Email used for the sign-in. Trimmed before being stored. */
  email: string;
  /** Current value of the remember-me checkbox. */
  rememberEmail: boolean;
}

export function applyRememberMeAfterSignIn(input: ApplyRememberMeInput): void {
  // Social / SSO: clear unconditionally. Returning here keeps the
  // remember-me checkbox state irrelevant for non-password methods —
  // the policy doesn't care what the user picked, the field isn't
  // useful for them either way.
  if (input.method !== 'password') {
    window.localStorage.removeItem(LAST_EMAIL_STORAGE_KEY);
    return;
  }

  if (!input.rememberEmail) {
    window.localStorage.removeItem(LAST_EMAIL_STORAGE_KEY);
    return;
  }

  // Remember-me ON for a password sign-in: store the trimmed email.
  // Empty strings are a no-op — defensive against a future path
  // calling this without an email; we'd rather preserve any prior
  // valid value than wipe it because of a bug at the call site.
  const trimmed = input.email.trim();
  if (trimmed) {
    window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, trimmed);
  }
}
