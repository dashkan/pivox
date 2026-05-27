// @vitest-environment jsdom
/**
 * Tests for the pre-hydration boot script builder.
 *
 * The script runs synchronously in <head>, before React mounts. For
 * each registered StorageItem:
 *   1. Reads the value from the platform's backend — cookie on
 *      http(s), localStorage otherwise.
 *   2. Invokes the item's onBoot (if defined) with the parsed value.
 *
 * The serialized output is JavaScript text that we exercise here by
 * evaluating it in jsdom — same execution path the inline <script>
 * tag will take in the browser.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { buildBootScript, defineItem } from '../src';
import { __resetRegistryForTests } from '../src/test-utils';

/**
 * Evaluate the generated boot script in the current jsdom realm.
 * Wrapping the `new Function()` call here keeps the unsafe-call
 * disable in one place — every test then runs through `runScript`
 * instead of stamping the disable comment on each call site.
 */
function runScript(src: string): void {
  // eslint-disable-next-line @typescript-eslint/no-implied-eval -- exercising the inline-script output
  const fn = new Function(src) as () => void;
  fn();
}

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

describe('buildBootScript (cookie backend — http://)', () => {
  beforeEach(() => {
    __resetRegistryForTests();
    clearAllStorage();
  });

  afterEach(() => {
    clearAllStorage();
  });

  it('reads from the cookie and invokes onBoot with the parsed value', () => {
    type Theme = 'light' | 'dark';
    defineItem<Theme>({
      name: 'pivox.test.theme',
      parse: (v) => (v === 'light' || v === 'dark' ? v : null),
      onBoot: (value) => {
        if (value === 'dark') {
          document.documentElement.setAttribute('data-boot-applied', 'dark');
        } else if (value === 'light') {
          document.documentElement.setAttribute('data-boot-applied', 'light');
        }
      },
    });
    document.cookie = `pivox.test.theme=dark; path=/`;

    runScript(buildBootScript());

    expect(document.documentElement.getAttribute('data-boot-applied')).toBe(
      'dark',
    );
    document.documentElement.removeAttribute('data-boot-applied');
  });

  it('invokes onBoot with null when the cookie is absent', () => {
    defineItem<string>({
      name: 'pivox.test.absent-onboot',
      parse: (v) => v || null,
      onBoot: (value) => {
        (window as unknown as { __received: string | null }).__received = value;
      },
    });

    runScript(buildBootScript());

    const received = (window as unknown as { __received: string | null })
      .__received;
    expect(received).toBeNull();
  });

  it('does NOT fall back to localStorage on a cookie-backend platform', () => {
    // Single-backend: web only reads the cookie. localStorage value
    // wouldn't normally exist on web post-migration; if it does,
    // it's ignored.
    let received: string | null = null;
    defineItem<string>({
      name: 'pivox.test.local-only',
      parse: (v) => v || null,
      onBoot: (value) => {
        (window as unknown as { __local: string | null }).__local = value;
      },
    });
    window.localStorage.setItem('pivox.test.local-only', 'should-be-ignored');

    runScript(buildBootScript());

    received = (window as unknown as { __local: string | null }).__local;
    expect(received).toBeNull();
  });

  it('escapes literal </script> in serialized parse / onBoot bodies', () => {
    // If a parse function's source contains `</script>` (string lit,
    // preserved comment, or anywhere outside a regex), the HTML
    // parser would terminate the inline <script> tag on that sequence
    // and emit the rest as text — XSS surface + broken script. The
    // escape turns `</script` into `<\/script` which is identical JS
    // but invisible to the HTML tokenizer.
    //
    // Test vector specifics:
    //   - String literal: V8's toString() preserves it verbatim, so
    //     the serialized source contains the raw `</script>` — exactly
    //     the input the escape must rewrite. (Regex literals do NOT
    //     work as test vectors here: V8 serializes a regex `/<\/...` as
    //     `/<\/.../` with the slash already backslash-escaped per
    //     RegExp literal syntax, which is invisible to the escape
    //     regex `/<\/script/gi` — the test would pass for the wrong
    //     reason, with the escape pathway never exercised.)
    defineItem<string>({
      name: 'pivox.test.script-tag',
      parse: (v) => (v === '</script>' ? null : v),
    });

    const out = buildBootScript();

    // The literal closing-tag sequence MUST NOT appear in the
    // serialized output — that's what the HTML parser would catch.
    expect(out).not.toMatch(/<\/script>/i);
    // The escaped form is what should appear instead. The presence
    // of the marker proves the .replace() actually fired (not just
    // that the input happened to not contain the unescaped form).
    expect(out).toContain('<\\/script>');
  });

  it('continues to subsequent items if one throws (per-item try/catch)', () => {
    defineItem<string>({
      name: 'pivox.test.thrower',
      parse: (v) => v || null,
      onBoot: () => {
        throw new Error('boom');
      },
    });
    defineItem<string>({
      name: 'pivox.test.after-thrower',
      parse: (v) => v || null,
      onBoot: (value) => {
        (window as unknown as { __after: string | null }).__after = value;
      },
    });
    document.cookie = `pivox.test.thrower=any; path=/`;
    document.cookie = `pivox.test.after-thrower=runs; path=/`;

    // Suppress the expected console.error from the script's error
    // logging — keeps test output clean.
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      runScript(buildBootScript());
    } finally {
      errSpy.mockRestore();
    }

    const after = (window as unknown as { __after: string | null }).__after;
    expect(after).toBe('runs');
  });
});

describe('buildBootScript (localStorage backend — file://)', () => {
  // See storage.test.ts for the rationale on replacing window.location
  // wholesale instead of patching a non-configurable getter.
  const originalLocation = window.location;

  beforeEach(() => {
    __resetRegistryForTests();
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

  it('reads from localStorage and invokes onBoot with the parsed value', () => {
    type Theme = 'light' | 'dark';
    defineItem<Theme>({
      name: 'pivox.test.electron-theme',
      parse: (v) => (v === 'light' || v === 'dark' ? v : null),
      onBoot: (value) => {
        if (value === 'dark') {
          document.documentElement.setAttribute('data-boot-applied', 'dark');
        }
      },
    });
    window.localStorage.setItem('pivox.test.electron-theme', 'dark');

    runScript(buildBootScript());

    expect(document.documentElement.getAttribute('data-boot-applied')).toBe(
      'dark',
    );
    document.documentElement.removeAttribute('data-boot-applied');
  });

  it('does NOT fall back to cookie on a localStorage-backend platform', () => {
    defineItem<string>({
      name: 'pivox.test.cookie-on-electron',
      parse: (v) => v || null,
      onBoot: (value) => {
        (window as unknown as { __electronGot: string | null }).__electronGot =
          value;
      },
    });
    document.cookie = `pivox.test.cookie-on-electron=ignored; path=/`;

    runScript(buildBootScript());

    const got = (window as unknown as { __electronGot: string | null })
      .__electronGot;
    expect(got).toBeNull();
  });
});
