import { describe, expect, it } from 'vitest';

import { isSpaceScoped, resourcePathParams } from '@/workflows/resource-paths';

describe('resourcePathParams', () => {
  it('maps an org-scoped collection parent', () => {
    expect(resourcePathParams('organizations/acme')).toEqual({
      organization: 'acme',
    });
  });

  it('maps a space-scoped collection parent', () => {
    expect(resourcePathParams('organizations/acme/spaces/main')).toEqual({
      organization: 'acme',
      space: 'main',
    });
  });

  it('maps an org-scoped leaf resource', () => {
    expect(resourcePathParams('organizations/acme/workflows/wf1')).toEqual({
      organization: 'acme',
      workflow: 'wf1',
    });
  });

  it('maps a space-scoped nested run', () => {
    expect(
      resourcePathParams(
        'organizations/acme/spaces/main/workflows/wf1/runs/r1',
      ),
    ).toEqual({
      organization: 'acme',
      space: 'main',
      workflow: 'wf1',
      run: 'r1',
    });
  });

  it('maps a version resource', () => {
    expect(
      resourcePathParams('organizations/acme/workflows/wf1/versions/v2'),
    ).toEqual({ organization: 'acme', workflow: 'wf1', version: 'v2' });
  });

  it('maps connector and secret leaves', () => {
    expect(resourcePathParams('organizations/acme/connectors/c1')).toEqual({
      organization: 'acme',
      connector: 'c1',
    });
    expect(resourcePathParams('organizations/acme/secrets/s1')).toEqual({
      organization: 'acme',
      secret: 's1',
    });
  });

  it('throws on an unknown collection', () => {
    expect(() => resourcePathParams('widgets/w1')).toThrow(
      /unknown collection "widgets"/,
    );
  });

  it('propagates malformed-name errors from parseResourceName', () => {
    expect(() => resourcePathParams('organizations')).toThrow();
  });
});

describe('isSpaceScoped', () => {
  it('is false for an org-scoped name', () => {
    expect(isSpaceScoped('organizations/acme/workflows/wf1')).toBe(false);
  });

  it('is true for a space-scoped name', () => {
    expect(
      isSpaceScoped('organizations/acme/spaces/main/workflows/wf1'),
    ).toBe(true);
  });
});
