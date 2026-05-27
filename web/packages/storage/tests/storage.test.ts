// @vitest-environment jsdom
/**
 * Tests for the @pivox/storage abstraction.
 *
 * Backend selection: jsdom defaults the document URL to
 * `http://localhost/`, so `location.protocol === 'http:'` and the
 * cookie backend is selected throughout these tests. The
 * "uses localStorage on non-http origins" case is covered in a
 * separate describe block by stubbing `location.protocol`.
 *
 * Each test resets the registry + clears storage so cases don't
 * leak state. The dev-time duplicate-name check is module-level
 * state — without the reset, a single registry would accumulate
 * items across tests and the duplicate-name test would
 * false-positive on later cases.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { allItems, clear, defineItem, get, set, storage } from '../src';
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

describe('defineItem', () => {
  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
  });

  it('registers the item and exposes it via allItems', () => {
    const item = defineItem<string>({
      name: 'pivox.test.greeting',
      parse: (v) => v || null,
    });
    expect(allItems()).toHaveLength(1);
    expect(allItems()[0]).toBe(item);
  });

  it('defaults path to `/` and maxAge to 1 year', () => {
    const item = defineItem<string>({
      name: 'pivox.test.defaults',
      parse: (v) => v,
    });
    expect(item.path).toBe('/');
    expect(item.maxAge).toBe(60 * 60 * 24 * 365);
  });

  it('honors explicit path and maxAge', () => {
    const item = defineItem<string>({
      name: 'pivox.test.scoped',
      path: '/auth/login',
      maxAge: 60,
      parse: (v) => v,
    });
    expect(item.path).toBe('/auth/login');
    expect(item.maxAge).toBe(60);
  });

  it('replaces on duplicate name (last-write-wins; HMR-safe)', () => {
    // Vite HMR re-executes items.ts when it changes, so a throw on
    // re-registration would break the dev loop. Last-write-wins lets
    // re-registration succeed; allItems() still returns one entry per
    // name (the most recent definition).
    const first = defineItem<string>({
      name: 'pivox.test.dup',
      parse: () => 'first',
    });
    const second = defineItem<string>({
      name: 'pivox.test.dup',
      parse: () => 'second',
    });

    const items = allItems();
    expect(items).toHaveLength(1);
    expect(items[0]).toBe(second);
    expect(items[0]).not.toBe(first);
    expect(items[0]?.parse('any')).toBe('second');
  });

  it('returns frozen items (defensive against accidental mutation)', () => {
    const item = defineItem<string>({
      name: 'pivox.test.frozen',
      parse: (v) => v,
    });
    expect(Object.isFrozen(item)).toBe(true);
  });
});

describe('get / set / clear (cookie backend — jsdom default http://)', () => {
  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
    clearAllStorage();
  });

  afterEach(() => {
    clearAllStorage();
  });

  const FOO = (): ReturnType<typeof defineItem<string>> =>
    defineItem<string>({
      name: 'pivox.test.foo',
      parse: (v) => (v.length > 0 ? v : null),
    });

  it('set writes to the cookie, NOT to localStorage', () => {
    const item = FOO();
    set(item, 'hello');
    expect(document.cookie).toContain(`${item.name}=hello`);
    // Single-backend: localStorage is untouched on web.
    expect(window.localStorage.getItem(item.name)).toBeNull();
  });

  it('set URL-encodes the cookie value', () => {
    const item = FOO();
    set(item, 'a@b.com');
    expect(document.cookie).toContain(`${item.name}=a%40b.com`);
  });

  it('get reads from the cookie', () => {
    const item = FOO();
    document.cookie = `${item.name}=cookie-value; path=/`;
    expect(get(item)).toBe('cookie-value');
  });

  it('get ignores a localStorage value on a cookie-backend platform', () => {
    // Web doesn't fall back to localStorage. If only localStorage
    // has the value (which shouldn't happen post-migration), get
    // returns null.
    const item = FOO();
    window.localStorage.setItem(item.name, 'local-value');
    expect(get(item)).toBeNull();
  });

  it('get returns null when the cookie is absent', () => {
    const item = FOO();
    expect(get(item)).toBeNull();
  });

  it('get URL-decodes cookie values', () => {
    const item = FOO();
    document.cookie = `${item.name}=a%40b.com; path=/`;
    expect(get(item)).toBe('a@b.com');
  });

  it('get returns null when parse rejects the value', () => {
    __resetRegistryForTests();
    __resetChannelForTests();
    type Theme = 'light' | 'dark';
    const themeItem = defineItem<Theme>({
      name: 'pivox.test.theme',
      parse: (v) => (v === 'light' || v === 'dark' ? v : null),
    });
    document.cookie = `${themeItem.name}=garbage; path=/`;
    expect(get(themeItem)).toBeNull();
  });

  it('get returns null on malformed percent-encoded cookie (no crash)', () => {
    const item = FOO();
    document.cookie = `${item.name}=%; path=/`;
    expect(get(item)).toBeNull();
  });

  it('get tolerates cookie separators with no space after the semicolon', () => {
    // RFC 6265 §4.2.1 specifies `; ` as the cookie-pair separator,
    // but writers in the wild sometimes emit `;` only. The internal
    // split pattern is `/;\s*/` (not literal `'; '`) so a leading-
    // space-less second cookie still matches.
    const item = FOO();
    // jsdom's document.cookie setter parses each `set` independently;
    // the round-trip merges them into the cookie jar with whatever
    // separator jsdom uses on read. To exercise the no-space case
    // directly, stub the cookie getter for this assertion.
    const desc = Object.getOwnPropertyDescriptor(Document.prototype, 'cookie');
    // Re-bind the original setter to `document` so jsdom's internal
    // `this` reference is preserved when the eslint "unbound-method"
    // rule doesn't flag the literal pass-through.
    const passthroughSet = (v: string): void => {
      desc?.set?.call(document, v);
    };
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get: () => `unrelated=x;${item.name}=present`,
      set: passthroughSet,
    });
    try {
      expect(get(item)).toBe('present');
    } finally {
      Reflect.deleteProperty(document, 'cookie');
    }
  });

  it('clear empties the cookie', () => {
    const item = FOO();
    set(item, 'present');
    expect(get(item)).toBe('present');
    clear(item);
    expect(get(item)).toBeNull();
  });

  it('storage namespace object aliases get / set / clear', () => {
    const item = FOO();
    storage.set(item, 'via-namespace');
    expect(storage.get(item)).toBe('via-namespace');
    storage.clear(item);
    expect(storage.get(item)).toBeNull();
  });

  it('set throws when the value would not round-trip through parse', () => {
    // FOO's parse rejects empty strings — setting '' would silently
    // disappear on the next get(). The throw surfaces the bug at the
    // call site instead.
    const item = FOO();
    expect(() => {
      set(item, '');
    }).toThrow(/parse\(\) rejects/);
  });

  it('get returns null when parse throws (catches instead of crashing)', () => {
    // Storage is mount-critical: useState lazy initializers call
    // get() during render. A throwing parse must not propagate to
    // React or the whole subtree crashes.
    const throwingItem = defineItem<string>({
      name: 'pivox.test.thrower',
      parse: () => {
        throw new Error('boom');
      },
    });
    document.cookie = `${throwingItem.name}=anything; path=/`;

    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      expect(get(throwingItem)).toBeNull();
    } finally {
      errSpy.mockRestore();
    }
  });

  it('set throws the lossy-round-trip error when parse throws', () => {
    // parse-throws is treated identically to parse-returns-null for
    // round-trip purposes: the next get() would return null, so the
    // write is lossy in both cases. Same error surface.
    const throwingItem = defineItem<string>({
      name: 'pivox.test.set-thrower',
      parse: () => {
        throw new Error('parse hates this');
      },
    });
    expect(() => {
      set(throwingItem, 'any');
    }).toThrow(/round-trip would be lossy/);
  });
});

/**
 * Cookie attribute coverage.
 *
 * jsdom's `document.cookie` getter exposes only the name=value pairs
 * that would be sent to a server — it does NOT report `path`,
 * `max-age`, `samesite`, or `secure` attributes once the cookie has
 * been stored. jsdom also doesn't enforce path-matching when reading,
 * so a `path=/auth/login` cookie is visible from `/` in tests. Both
 * mean "we wrote with the right attributes" can't be validated by
 * reading the cookie back.
 *
 * Intercept the `Document.prototype.cookie` setter for the duration
 * of these tests: every `set()` / `clear()` call routes through that
 * setter with the full cookie string (`name=value; path=...; ...`),
 * so we can capture the exact write and assert on each attribute.
 * Restore in afterEach.
 *
 * Coverage rationale: someone changing an item's `path` (or any
 * attribute) silently — by typo, refactor, or future "tighten the
 * scope" pass — must be caught by tests. Reading `document.cookie`
 * back can't catch it. Intercepting the setter can.
 */
describe('cookie attribute writes (intercept Document.prototype.cookie setter)', () => {
  let cookieDescriptor: PropertyDescriptor | undefined;
  let writes: string[] = [];

  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
    clearAllStorage();
    writes = [];
    cookieDescriptor = Object.getOwnPropertyDescriptor(
      Document.prototype,
      'cookie',
    );
    // Wrap the setter — capture every write while still passing through
    // to jsdom's real setter so document.cookie keeps its normal
    // semantics for the rest of the suite.
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get(): string {
        const v: unknown = cookieDescriptor?.get?.call(document);
        return typeof v === 'string' ? v : '';
      },
      set(v: string) {
        writes.push(v);
        cookieDescriptor?.set?.call(document, v);
      },
    });
  });

  afterEach(() => {
    // Restore the original descriptor on Document.prototype. The
    // per-instance override we set in beforeEach shadows the prototype
    // accessor; delete the instance property to fall through.
    Reflect.deleteProperty(document, 'cookie');
    clearAllStorage();
  });

  it('write includes path=/ by default', () => {
    const item = defineItem<string>({
      name: 'pivox.test.default-path',
      parse: (v) => v || null,
    });
    set(item, 'x');
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatch(/\bpath=\/(;|$)/);
  });

  it('write includes the item-declared path verbatim', () => {
    const item = defineItem<string>({
      name: 'pivox.test.scoped-path',
      path: '/auth/login',
      parse: (v) => v || null,
    });
    set(item, 'x');
    expect(writes[0]).toContain('path=/auth/login');
  });

  it('write includes samesite=lax', () => {
    const item = defineItem<string>({
      name: 'pivox.test.samesite',
      parse: (v) => v || null,
    });
    set(item, 'x');
    expect(writes[0]).toContain('samesite=lax');
  });

  it('write includes the item-declared max-age', () => {
    const item = defineItem<string>({
      name: 'pivox.test.maxage',
      maxAge: 60,
      parse: (v) => v || null,
    });
    set(item, 'x');
    expect(writes[0]).toMatch(/\bmax-age=60\b/);
  });

  it('write defaults max-age to 1 year (31536000s)', () => {
    const item = defineItem<string>({
      name: 'pivox.test.default-maxage',
      parse: (v) => v || null,
    });
    set(item, 'x');
    expect(writes[0]).toMatch(/\bmax-age=31536000\b/);
  });

  it('write omits secure on http:// (jsdom default)', () => {
    const item = defineItem<string>({
      // Name deliberately avoids the substring "secure" so the assertion
      // below doesn't false-positive on a name match.
      name: 'pivox.test.http-only',
      parse: (v) => v || null,
    });
    set(item, 'x');
    // Match the `; secure` attribute specifically, not the substring
    // (cookie names can legally contain the word "secure").
    expect(writes[0]).not.toMatch(/;\s*secure(\s*;|\s*$)/i);
  });

  it('clear uses max-age=0 with matching path', () => {
    const item = defineItem<string>({
      name: 'pivox.test.clear-attrs',
      path: '/auth/login',
      parse: (v) => v || null,
    });
    set(item, 'x');
    clear(item);
    // First write is the set; second is the clear.
    expect(writes).toHaveLength(2);
    expect(writes[1]).toMatch(/\bmax-age=0\b/);
    expect(writes[1]).toContain('path=/auth/login');
  });
});

describe('get / set / clear (localStorage backend — file://)', () => {
  // jsdom marks `window.location.protocol` as non-configurable, so we
  // can't override the getter. Replace `window.location` wholesale
  // with a stand-in object that reports `file:` — the storage module
  // only reads `location.protocol`, nothing else, so a minimal stub
  // is sufficient. Restored in afterEach.
  const originalLocation = window.location;

  beforeEach(() => {
    __resetRegistryForTests();
    __resetChannelForTests();
    clearAllStorage();
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { protocol: 'file:' },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: originalLocation,
    });
    clearAllStorage();
  });

  const FOO = (): ReturnType<typeof defineItem<string>> =>
    defineItem<string>({
      name: 'pivox.test.electron',
      parse: (v) => (v.length > 0 ? v : null),
    });

  it('set writes to localStorage, NOT to cookie', () => {
    const item = FOO();
    set(item, 'electron-value');
    expect(window.localStorage.getItem(item.name)).toBe('electron-value');
    expect(document.cookie).not.toContain(`${item.name}=`);
  });

  it('get reads from localStorage', () => {
    const item = FOO();
    window.localStorage.setItem(item.name, 'from-local');
    expect(get(item)).toBe('from-local');
  });

  it('get ignores a cookie value on a localStorage-backend platform', () => {
    const item = FOO();
    // Cookie won't actually persist on file:// in real electron, but
    // for the test we set it to verify the backend ignores cookies.
    document.cookie = `${item.name}=cookie-value; path=/`;
    expect(get(item)).toBeNull();
  });

  it('clear empties localStorage', () => {
    const item = FOO();
    set(item, 'present');
    expect(get(item)).toBe('present');
    clear(item);
    expect(get(item)).toBeNull();
    expect(window.localStorage.getItem(item.name)).toBeNull();
  });
});
