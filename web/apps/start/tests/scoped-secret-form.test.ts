import { describe, expect, it } from 'vitest';

import { scopedSecretsListRoute } from '../src/features/secrets/scoped-secret-form';

// The `?from=` fallback target for the scoped secret form pages — the list the
// create/edit forms return to when no launching route was carried. This is the
// scoped analogue of the connectors form's `scopedListRoute`; the org-vs-space
// branch is the return-nav coherence the routed forms depend on.
describe('scopedSecretsListRoute', () => {
  it('returns the org-rollup secrets list when no space is given', () => {
    expect(scopedSecretsListRoute('acme')).toBe('/organizations/acme/secrets');
    expect(scopedSecretsListRoute('acme', undefined)).toBe(
      '/organizations/acme/secrets',
    );
  });

  it('returns the space-scoped secrets list when a space is given', () => {
    expect(scopedSecretsListRoute('acme', 'dev')).toBe(
      '/organizations/acme/spaces/dev/secrets',
    );
  });
});
