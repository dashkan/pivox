// @vitest-environment jsdom
// @vitest-environment-options { "url": "http://localhost/auth/login" }
/**
 * Behavioral spec for the remember-me policy across sign-in methods.
 *
 * jsdom URL is pinned to `/auth/login` because LAST_EMAIL is path-
 * scoped (see @pivox/storage's items.ts) — cookies with that path
 * aren't visible from `/`, so tests run under the login route the
 * cookie was scoped to. Same constraint applies in production: only
 * the login page reads this slot.
 *
 * Storage mechanics (cookie + localStorage, encoding, paths) are
 * @pivox/storage's responsibility and tested there. These tests cover
 * the POLICY layer — which sign-in methods write vs clear the slot.
 *
 * Coverage:
 *   - Password + remember ON → stored
 *   - Password + remember OFF → cleared
 *   - Social → always cleared
 *   - SSO → always cleared
 *   - Empty-email edge case (preserve on ON, clear on OFF)
 */
import { LAST_EMAIL, storage } from '@pivox/storage';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { applyRememberMeAfterSignIn } from '@/login/remember-email';

describe('applyRememberMeAfterSignIn', () => {
  beforeEach(() => {
    storage.clear(LAST_EMAIL);
  });

  afterEach(() => {
    storage.clear(LAST_EMAIL);
  });

  describe('password sign-in', () => {
    it('stores the email when remember-me is ON', () => {
      applyRememberMeAfterSignIn({
        method: 'password',
        email: 'user@example.com',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBe('user@example.com');
    });

    it('trims whitespace before storing', () => {
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '  user@example.com  ',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBe('user@example.com');
    });

    it('clears the slot when remember-me is OFF', () => {
      storage.set(LAST_EMAIL, 'stale@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: 'user@example.com',
        rememberEmail: false,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });

    it('does not write an empty email even when remember-me is ON', () => {
      // Defensive — every known sign-in path carries an email, but
      // an empty string shouldn't poison the slot if one ever slips
      // through. Existing value is preserved.
      storage.set(LAST_EMAIL, 'prev@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBe('prev@example.com');
    });

    it('clears the slot when remember-me is OFF even with an empty email', () => {
      // Pin the order-of-checks: remember-off is an explicit opt-out
      // that wipes the slot regardless of email content. A future
      // refactor that re-orders the empty-email check above the
      // remember-off check would flip this case to "preserve", and
      // this test would catch it.
      storage.set(LAST_EMAIL, 'prev@example.com');
      applyRememberMeAfterSignIn({
        method: 'password',
        email: '',
        rememberEmail: false,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });
  });

  describe('social sign-in', () => {
    it('clears the slot regardless of remember-me state', () => {
      storage.set(LAST_EMAIL, 'pwd@example.com');
      applyRememberMeAfterSignIn({
        method: 'social',
        email: 'social@example.com',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });

    it('does not store the social email even with remember-me ON', () => {
      applyRememberMeAfterSignIn({
        method: 'social',
        email: 'social@example.com',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });
  });

  describe('SSO sign-in', () => {
    it('clears the slot regardless of remember-me state', () => {
      storage.set(LAST_EMAIL, 'pwd@example.com');
      applyRememberMeAfterSignIn({
        method: 'sso',
        email: 'sso@example.com',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });

    it('does not store the SSO email even with remember-me ON', () => {
      applyRememberMeAfterSignIn({
        method: 'sso',
        email: 'sso@example.com',
        rememberEmail: true,
      });
      expect(storage.get(LAST_EMAIL)).toBeNull();
    });
  });
});
