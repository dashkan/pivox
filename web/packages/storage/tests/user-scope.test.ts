// @vitest-environment jsdom
/**
 * Tests for user-scoped storage items + clearUserScopedItems().
 *
 * Background: items like ACTIVE_ORG hold per-user state. On sign-out
 * they MUST be cleared so the next user doesn't inherit the previous
 * user's selection (the bug this guards against). Device-scoped items
 * (theme, sidebar) must SURVIVE sign-out — they're per-device prefs.
 *
 * jsdom defaults the URL to http://localhost/, so the cookie backend
 * is selected throughout. Registry + storage are reset per test.
 */
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import {
  ACTIVE_ORG,
  clearUserScopedItems,
  defineItem,
  get,
  LAST_EMAIL,
  set,
  SIDEBAR_OPEN,
  THEME,
} from '../src';
import {
  __resetChannelForTests,
  __resetRegistryForTests,
} from '../src/test-utils';

function clearAllStorage(): void {
  document.cookie.split('; ').forEach((entry) => {
    const eq = entry.indexOf('=');
    const name = eq > 0 ? entry.slice(0, eq) : entry;
    if (name) {
      document.cookie = `${name}=; path=/; max-age=0`;
      document.cookie = `${name}=; path=/auth/login; max-age=0`;
    }
  });
  window.localStorage.clear();
}

describe('defineItem scope', () => {
  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
  });

  it('defaults scope to "device"', () => {
    const item = defineItem<string>({
      name: 'pivox.test.default-scope',
      parse: (v) => v || null,
    });
    expect(item.scope).toBe('device');
  });

  it('honors explicit scope: "user"', () => {
    const item = defineItem<string>({
      name: 'pivox.test.user-scope',
      scope: 'user',
      parse: (v) => v || null,
    });
    expect(item.scope).toBe('user');
  });
});

describe('clearUserScopedItems', () => {
  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
    clearAllStorage();
  });
  afterEach(() => {
    clearAllStorage();
  });

  it('clears user-scoped items but preserves device-scoped ones', () => {
    const userItem = defineItem<string>({
      name: 'pivox.test.user',
      scope: 'user',
      parse: (v) => v || null,
    });
    const deviceItem = defineItem<string>({
      name: 'pivox.test.device',
      scope: 'device',
      parse: (v) => v || null,
    });

    set(userItem, 'u');
    set(deviceItem, 'd');
    expect(get(userItem)).toBe('u');
    expect(get(deviceItem)).toBe('d');

    clearUserScopedItems();

    expect(get(userItem)).toBeNull();
    expect(get(deviceItem)).toBe('d');
  });

  it('is a no-op when there are no user-scoped items', () => {
    const deviceItem = defineItem<string>({
      name: 'pivox.test.device-only',
      parse: (v) => v || null,
    });
    set(deviceItem, 'keep');
    expect(() => clearUserScopedItems()).not.toThrow();
    expect(get(deviceItem)).toBe('keep');
  });
});

describe('real item scopes (the contract sign-out relies on)', () => {
  it('ACTIVE_ORG is user-scoped; theme/sidebar/last-email are device-scoped', () => {
    expect(ACTIVE_ORG.scope).toBe('user');
    expect(THEME.scope).toBe('device');
    expect(SIDEBAR_OPEN.scope).toBe('device');
    expect(LAST_EMAIL.scope).toBe('device');
  });
});
