/**
 * Policy for the login auto-fill email slot.
 *
 * Storage mechanics (cookie + localStorage dual-store, encoding,
 * SSR-safety) are owned by `@pivox/storage`. This file is JUST the
 * policy:
 *   - Password sign-in: respect the remember-me checkbox. ON stores
 *     the email; OFF clears it (explicit opt-out wipes the slot).
 *   - Social or SSO sign-in: always clear, regardless of checkbox.
 *     The auto-fill field isn't useful for those flows (social uses
 *     provider buttons, SSO re-enters the email each time to
 *     discover the provider), and a stale password-era value would
 *     confuse subsequent sessions.
 *
 * Extracted from `use-login.ts` so the decision logic is testable
 * in isolation — the hook's React state machinery doesn't need to
 * be in the test loop.
 */

import { LAST_EMAIL, storage } from '@pivox/storage';

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
  // Social / SSO: clear unconditionally. The remember-me checkbox is
  // irrelevant for non-password methods.
  if (input.method !== 'password') {
    storage.clear(LAST_EMAIL);
    return;
  }

  if (!input.rememberEmail) {
    storage.clear(LAST_EMAIL);
    return;
  }

  // Remember-me ON for a password sign-in: store the trimmed email.
  // Empty strings are a no-op — defensive against a future path
  // calling this without an email; we'd rather preserve any prior
  // valid value than wipe it because of a bug at the call site.
  const trimmed = input.email.trim();
  if (trimmed) {
    storage.set(LAST_EMAIL, trimmed);
  }
}
