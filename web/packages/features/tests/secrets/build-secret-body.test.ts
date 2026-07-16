import { describe, expect, it } from 'vitest';

import {
  buildSecretCreateBody,
  buildSecretUpdateBody,
} from '@/secrets/build-secret-body';

import type { SecretFormValues } from '@pivox/ui/resource-admin';

const base: SecretFormValues = {
  secretId: 'stripe-key',
  displayName: 'Stripe key',
  annotations: [],
  value: 's3cr3t',
  rotate: false,
};

describe('buildSecretCreateBody', () => {
  it('base64-encodes the value on create (proto `bytes` field)', () => {
    // protojson requires `bytes` fields base64-encoded; the raw string is
    // rejected. A value with a space AND a non-ASCII char proves the encoder
    // is UTF-8-safe, not naive `btoa`.
    const body = buildSecretCreateBody({ ...base, value: 'My Sécret' });
    expect(body.value).toBe('TXkgU8OpY3JldA==');
  });

  it('omits annotations when there are no non-blank rows', () => {
    expect(buildSecretCreateBody(base).annotations).toBeUndefined();
  });

  it('includes annotations when present', () => {
    const body = buildSecretCreateBody({
      ...base,
      annotations: [{ key: 'env', value: 'prod' }],
    });
    expect(body.annotations).toEqual({ env: 'prod' });
  });
});

describe('buildSecretUpdateBody (field-mask assembly)', () => {
  it('OMITS value for a metadata-only edit (rotate=false)', () => {
    const body = buildSecretUpdateBody({ values: base, etag: 'e1' });
    expect(body).not.toHaveProperty('value');
    expect(body.displayName).toBe('Stripe key');
    expect(body.etag).toBe('e1');
  });

  it('INCLUDES the value base64-encoded when rotating (rotate=true)', () => {
    const body = buildSecretUpdateBody({
      values: { ...base, rotate: true, value: 'My Sécret' },
    });
    expect(body.value).toBe('TXkgU8OpY3JldA==');
  });

  it('always names annotations (mask covers a full replace of metadata)', () => {
    const body = buildSecretUpdateBody({
      values: { ...base, annotations: [{ key: 'env', value: 'prod' }] },
    });
    expect(body.annotations).toEqual({ env: 'prod' });
  });

  it('omits etag when not provided', () => {
    expect(buildSecretUpdateBody({ values: base })).not.toHaveProperty('etag');
  });
});
