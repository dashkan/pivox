import { describe, expect, it } from 'vitest';

import { resolveOrgBySlug, resolveRootTarget } from '../src/lib/scope-resolve';

const ACME = { organization: 'organizations/acme', displayName: 'Acme' };
const GLOBEX = { organization: 'organizations/globex', displayName: 'Globex' };

describe('resolveRootTarget', () => {
  it('sends a zero-org user to forced create-org, even with a stale cookie', () => {
    expect(resolveRootTarget([], 'organizations/gone')).toEqual({
      kind: 'create',
    });
  });

  it('redirects to the remembered org when it is still a membership', () => {
    expect(resolveRootTarget([ACME, GLOBEX], 'organizations/globex')).toEqual({
      kind: 'org',
      organization: 'organizations/globex',
    });
  });

  it('falls to the selector when the remembered org is stale', () => {
    expect(resolveRootTarget([ACME], 'organizations/globex')).toEqual({
      kind: 'selector',
    });
  });

  it('falls to the selector when nothing is remembered', () => {
    expect(resolveRootTarget([ACME], null)).toEqual({ kind: 'selector' });
  });
});

describe('resolveOrgBySlug', () => {
  it('resolves a slug to the matching membership', () => {
    expect(resolveOrgBySlug([ACME, GLOBEX], 'acme')).toBe(ACME);
  });

  it('returns null for a slug the caller does not belong to (no fallback)', () => {
    expect(resolveOrgBySlug([ACME], 'globex')).toBeNull();
  });

  it('returns null for an empty slug', () => {
    expect(resolveOrgBySlug([ACME], '')).toBeNull();
  });
});
