import { describe, expect, it } from 'vitest';

import {
  organizationId,
  parseResourceName,
  spaceId,
} from '../src/resource';

describe('parseResourceName', () => {
  it('parses a single collection/id pair', () => {
    expect(parseResourceName('organizations/acme')).toEqual({
      organizations: 'acme',
    });
  });

  it('parses a nested collection/id chain', () => {
    expect(parseResourceName('organizations/acme/spaces/main')).toEqual({
      organizations: 'acme',
      spaces: 'main',
    });
  });

  it('parses deeper chains', () => {
    expect(
      parseResourceName(
        'organizations/acme/spaces/main/assets/logo/versions/v1',
      ),
    ).toEqual({
      organizations: 'acme',
      spaces: 'main',
      assets: 'logo',
      versions: 'v1',
    });
  });

  it('returns an empty map for empty input', () => {
    expect(parseResourceName('')).toEqual({});
  });

  it('throws on an odd number of segments', () => {
    expect(() => parseResourceName('organizations/acme/spaces')).toThrow(
      /even number of segments/,
    );
  });

  it('throws on an empty segment', () => {
    expect(() => parseResourceName('organizations//spaces/main')).toThrow(
      /empty segment/,
    );
  });

  it('throws on a duplicate collection name', () => {
    expect(() =>
      parseResourceName('organizations/acme/organizations/beta'),
    ).toThrow(/duplicate collection/);
  });
});

describe('organizationId', () => {
  it('extracts the slug from a bare org resource name', () => {
    expect(organizationId('organizations/acme')).toBe('acme');
  });

  it('extracts the org slug from a deeper resource name', () => {
    expect(organizationId('organizations/acme/spaces/main')).toBe('acme');
  });

  it('returns an empty string for empty input (guard for enabled flags)', () => {
    expect(organizationId('')).toBe('');
  });
});

describe('spaceId', () => {
  it('extracts the space slug from a space resource name', () => {
    expect(spaceId('organizations/acme/spaces/main')).toBe('main');
  });

  it('returns an empty string when the name has no space segment', () => {
    expect(spaceId('organizations/acme')).toBe('');
  });
});
