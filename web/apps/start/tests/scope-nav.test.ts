import { describe, expect, it } from 'vitest';

import {
  orgNavTarget,
  resourceFromPathname,
  spaceNavTarget,
} from '../src/lib/scope-nav';

describe('resourceFromPathname', () => {
  it('detects the connectors resource on org + space paths', () => {
    expect(resourceFromPathname('/organizations/acme/connectors')).toBe(
      'connectors',
    );
    expect(
      resourceFromPathname('/organizations/acme/spaces/dev/connectors/new'),
    ).toBe('connectors');
  });

  it('detects the secrets resource on org + space paths', () => {
    expect(resourceFromPathname('/organizations/acme/secrets')).toBe('secrets');
    expect(
      resourceFromPathname('/organizations/acme/spaces/dev/secrets/foo/edit'),
    ).toBe('secrets');
  });

  it('detects the workflows resource on org + space paths', () => {
    expect(resourceFromPathname('/organizations/acme/workflows')).toBe(
      'workflows',
    );
    expect(
      resourceFromPathname('/organizations/acme/spaces/dev/workflows/wf1'),
    ).toBe('workflows');
    expect(resourceFromPathname('/organizations/acme/workflows/wf1')).toBe(
      'workflows',
    );
  });

  it('returns null off a known resource (e.g. the org home)', () => {
    expect(resourceFromPathname('/organizations/acme')).toBeNull();
    expect(resourceFromPathname('/')).toBeNull();
  });

  // Org/space slugs are free-form, so one literally named after a resource must
  // NOT be mistaken for the resource segment (which sits at a fixed position).
  it('reads the resource by position, not slug-name collision', () => {
    expect(resourceFromPathname('/organizations/secrets/connectors')).toBe(
      'connectors',
    );
    expect(
      resourceFromPathname('/organizations/acme/spaces/workflows/connectors'),
    ).toBe('connectors');
    // An org named after a resource, sitting at the org home → still null.
    expect(resourceFromPathname('/organizations/connectors')).toBeNull();
  });
});

describe('spaceNavTarget', () => {
  it('connectors path → space connectors when a space is picked', () => {
    expect(spaceNavTarget('/organizations/acme/connectors', 'acme', 'dev')).toBe(
      '/organizations/acme/spaces/dev/connectors',
    );
  });

  it('secrets path → space secrets when a space is picked', () => {
    expect(spaceNavTarget('/organizations/acme/secrets', 'acme', 'dev')).toBe(
      '/organizations/acme/spaces/dev/secrets',
    );
  });

  it('null space → the org-rollup connectors route from a connectors path', () => {
    expect(
      spaceNavTarget('/organizations/acme/spaces/dev/connectors', 'acme', null),
    ).toBe('/organizations/acme/connectors');
  });

  it('null space → the org-rollup secrets route from a secrets path', () => {
    expect(
      spaceNavTarget('/organizations/acme/spaces/dev/secrets', 'acme', null),
    ).toBe('/organizations/acme/secrets');
  });

  it('workflows path → space/org workflows (workflows are space-scoped)', () => {
    expect(spaceNavTarget('/organizations/acme/workflows', 'acme', 'dev')).toBe(
      '/organizations/acme/spaces/dev/workflows',
    );
    expect(
      spaceNavTarget('/organizations/acme/spaces/dev/workflows/wf1', 'acme', null),
    ).toBe('/organizations/acme/workflows');
  });
});

describe('orgNavTarget', () => {
  it('keeps the current section at org-rollup scope in the new org', () => {
    // Switching org drops the space (a different org has different spaces).
    expect(
      orgNavTarget('/organizations/acme/spaces/dev/secrets', 'globex'),
    ).toBe('/organizations/globex/secrets');
    expect(orgNavTarget('/organizations/acme/connectors', 'globex')).toBe(
      '/organizations/globex/connectors',
    );
  });

  it('lands on the org home when the path is not under a known resource', () => {
    expect(orgNavTarget('/organizations/acme', 'globex')).toBe(
      '/organizations/globex',
    );
  });
});
