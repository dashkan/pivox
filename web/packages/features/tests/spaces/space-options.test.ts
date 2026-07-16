import { describe, expect, it } from 'vitest';

import type { components } from '@pivox/client/types';

import { toSpaceOptions } from '@/spaces/space-options';

type Space = components['schemas']['v1Space'];

describe('toSpaceOptions', () => {
  it('maps name → slug (leaf) and prefers displayName as the label', () => {
    const spaces: Space[] = [
      { name: 'organizations/acme/spaces/main', displayName: 'Main' },
      { name: 'organizations/acme/spaces/eu', displayName: '' },
    ];
    expect(toSpaceOptions(spaces)).toEqual([
      { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
      { name: 'organizations/acme/spaces/eu', slug: 'eu', displayName: 'eu' },
    ]);
  });

  it('drops spaces without a name', () => {
    expect(toSpaceOptions([{ displayName: 'nameless' }])).toEqual([]);
  });
});
