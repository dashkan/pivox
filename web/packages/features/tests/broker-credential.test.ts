import { describe, expect, it } from 'vitest';

import { buildBrokerCredential } from '@/shared/broker-credential';

describe('buildBrokerCredential', () => {
  it('builds a GitHub credential from an access token', () => {
    const credential = buildBrokerCredential({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 'gho_abc',
    });
    expect(credential.providerId).toBe('github.com');
    expect(credential.signInMethod).toBe('github.com');
  });

  it('builds a Google credential from an id token', () => {
    const credential = buildBrokerCredential({
      ok: true,
      provider: 'google',
      kind: 'google_id_token',
      token: 'id-token',
      accessToken: 'access-token',
    });
    expect(credential.providerId).toBe('google.com');
  });

  it('builds an OIDC credential carrying the broker provider id', () => {
    const credential = buildBrokerCredential({
      ok: true,
      provider: 'oidc.acme',
      kind: 'oidc_id_token',
      token: 'id-token',
      nonce: 'raw-nonce',
    });
    expect(credential.providerId).toBe('oidc.acme');
  });

  it('builds an OIDC credential when no nonce is present', () => {
    const credential = buildBrokerCredential({
      ok: true,
      provider: 'oidc.acme',
      kind: 'oidc_id_token',
      token: 'id-token',
    });
    expect(credential.providerId).toBe('oidc.acme');
  });

  it('builds a Google credential when no access token is present', () => {
    const credential = buildBrokerCredential({
      ok: true,
      provider: 'google',
      kind: 'google_id_token',
      token: 'id-token',
    });
    expect(credential.providerId).toBe('google.com');
  });
});
