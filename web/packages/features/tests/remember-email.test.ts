// @vitest-environment jsdom
/**
 * Behavioral spec for the remember-me policy across sign-in methods.
 *
 * The localStorage slot `pivox.login.last-email` auto-fills the email
 * field on next visit. Policy:
 *   - Password sign-in + remember-me ON  → store the email.
 *   - Password sign-in + remember-me OFF → clear any stored email.
 *   - Social or SSO sign-in              → always clear, regardless of
 *                                          the checkbox state.
 *
 * Social / SSO clear unconditionally because the email field is no
 * longer useful for those flows: social users click the provider
 * button (no email entry), and SSO users enter their email each time
 * to discover the provider. Leaving a stale password-era email in the
 * field would just confuse the next session.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  applyRememberMeAfterSignIn,
  LAST_EMAIL_STORAGE_KEY,
} from '@/login/remember-email';

describe('applyRememberMeAfterSignIn', () => {
  beforeEach(() => {
    // Each test starts with a clean slot; we exercise both write and
    // clear paths and need a known starting state.
    window.localStorage.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  describe('password sign-in', () => {
    it('stores the email when remember-me is ON', () => {
      applyRememberMeAfterSignIn({
        method: 'password',
        email: 'user@example.com',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBe(
        'user@example.com',
      );
    });

    it('trims whitespace before storing', () => {
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '  user@example.com  ',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBe(
        'user@example.com',
      );
    });

    it('clears the slot when remember-me is OFF', () => {
      window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, 'stale@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: 'user@example.com',
        rememberEmail: false,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });

    it('does not write an empty email even when remember-me is ON', () => {
      // Defensive — every known sign-in path carries an email, but
      // an empty string shouldn't poison the slot if one ever slips
      // through. Existing value is preserved (we neither write nor
      // unintentionally clear when the input is empty + remember-on).
      window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, 'prev@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBe(
        'prev@example.com',
      );
    });

    it('clears the slot when remember-me is OFF even with an empty email', () => {
      // Pin the order-of-checks: remember-off is an explicit opt-out
      // that wipes the slot regardless of email content. A future
      // refactor that re-orders the empty-email check above the
      // remember-off check would flip this case to "preserve", and
      // this test would catch it.
      window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, 'prev@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '',
        rememberEmail: false,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });
  });

  describe('social sign-in', () => {
    it('clears the slot regardless of remember-me state', () => {
      window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, 'pwd@example.com');
      applyRememberMeAfterSignIn({
        method: 'social',
        email: 'social@example.com',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });

    it('does not store the social email even with remember-me ON', () => {
      applyRememberMeAfterSignIn({
        method: 'social',
        email: 'social@example.com',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });
  });

  describe('SSO sign-in', () => {
    it('clears the slot regardless of remember-me state', () => {
      window.localStorage.setItem(LAST_EMAIL_STORAGE_KEY, 'pwd@example.com');
      applyRememberMeAfterSignIn({
        method: 'sso',
        email: 'sso@example.com',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });

    it('does not store the SSO email even with remember-me ON', () => {
      applyRememberMeAfterSignIn({
        method: 'sso',
        email: 'sso@example.com',
        rememberEmail: true,
      });
      expect(window.localStorage.getItem(LAST_EMAIL_STORAGE_KEY)).toBeNull();
    });
  });
});
