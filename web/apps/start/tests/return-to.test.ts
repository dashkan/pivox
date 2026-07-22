import { describe, expect, it } from 'vitest';

import { resolveReturnTo } from '../src/lib/return-to';

// `resolveReturnTo` is the route-owned open-redirect defense shared by every
// routed resource form: it sanitizes `from` via `safeInternalPath` and falls
// back to the GIVEN scoped list route (each caller passes its own). On the SSR
// pass (no window) it resolves against a placeholder origin, which is fine
// because a valid relative `from` yields the same pathname either way.
describe('resolveReturnTo — generic, parameterized fallback', () => {
  it('returns a valid same-app path unchanged, regardless of fallback', () => {
    expect(resolveReturnTo('/secrets?scope=main&q=x', '/secrets')).toBe(
      '/secrets?scope=main&q=x',
    );
  });

  it('falls back to the GIVEN route for an external URL', () => {
    expect(resolveReturnTo('https://evil.com/steal', '/secrets')).toBe(
      '/secrets',
    );
  });

  it('falls back to the given route for a protocol-relative URL', () => {
    expect(resolveReturnTo('//evil.com', '/secrets')).toBe('/secrets');
  });

  it('falls back to the given route for a backslash-normalization trick', () => {
    expect(resolveReturnTo('/\\evil.com', '/secrets')).toBe('/secrets');
  });

  it('falls back to the given route when `from` is absent', () => {
    expect(resolveReturnTo(undefined, '/secrets')).toBe('/secrets');
  });
});
