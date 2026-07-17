import { describe, expect, it } from 'vitest';

import { buildSecretFilter } from '@/secrets/build-secret-filter';

describe('buildSecretFilter', () => {
  it('returns undefined when no filter is active', () => {
    expect(buildSecretFilter({})).toBeUndefined();
    expect(buildSecretFilter({ displayName: '  ' })).toBeUndefined();
  });

  it('builds a substring predicate for displayName', () => {
    expect(buildSecretFilter({ displayName: 'stripe' })).toBe(
      'displayName:"stripe"',
    );
  });

  it('escapes a quote in the filter value (no expression break-out)', () => {
    expect(buildSecretFilter({ displayName: 'a"b' })).toBe(
      'displayName:"a\\"b"',
    );
  });
});
