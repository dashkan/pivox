import { describe, expect, it } from 'vitest';

import {
  CONNECTORS_LIST_ROUTE,
  resolveConnectorReturn,
  resolveReturnTo,
} from '../src/lib/return-to';

// `resolveConnectorReturn` is the route-owned open-redirect defense: it wraps
// `safeInternalPath` and falls back to the connectors list. On the SSR pass
// (no window) it resolves against a placeholder origin, which is fine because a
// valid relative `from` yields the same pathname either way.
describe('resolveConnectorReturn — accepts internal paths', () => {
  it('returns a valid same-app path (with search/scope) unchanged', () => {
    expect(resolveConnectorReturn('/connectors?scope=main&q=x')).toBe(
      '/connectors?scope=main&q=x',
    );
  });
});

describe('resolveConnectorReturn — falls back on unsafe / missing input', () => {
  it('falls back to the connectors list for an external URL', () => {
    expect(resolveConnectorReturn('https://evil.com/steal')).toBe(
      CONNECTORS_LIST_ROUTE,
    );
  });

  it('falls back for a protocol-relative URL', () => {
    expect(resolveConnectorReturn('//evil.com')).toBe(CONNECTORS_LIST_ROUTE);
  });

  it('falls back for a backslash-normalization trick', () => {
    expect(resolveConnectorReturn('/\\evil.com')).toBe(CONNECTORS_LIST_ROUTE);
  });

  it('falls back when `from` is absent', () => {
    expect(resolveConnectorReturn(undefined)).toBe(CONNECTORS_LIST_ROUTE);
  });
});

// The generic resolver backs BOTH connectors and secrets: it sanitizes the same
// way but takes the fallback list route as a parameter, so a second consumer
// (secrets) lands on its OWN list on an unsafe / missing `from`.
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
