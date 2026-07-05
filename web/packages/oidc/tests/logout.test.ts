import * as oidc from 'openid-client';
import { describe, expect, it } from 'vitest';


import { buildEndSessionUrl } from '@/logout';

function testConfig(): oidc.Configuration {
  return new oidc.Configuration(
    {
      issuer: 'https://kc.example/realms/pivox',
      authorization_endpoint: 'https://kc.example/realms/pivox/protocol/openid-connect/auth',
      token_endpoint: 'https://kc.example/realms/pivox/protocol/openid-connect/token',
      end_session_endpoint: 'https://kc.example/realms/pivox/protocol/openid-connect/logout',
    },
    'electron',
    undefined,
    oidc.None(),
  );
}

describe('buildEndSessionUrl', () => {
  it('builds the RP-initiated end-session URL with hint + post-logout redirect', () => {
    const url = buildEndSessionUrl(testConfig(), {
      postLogoutRedirectUri: 'pivox://signed-out',
      idTokenHint: 'the-id-token',
    });
    expect(url.origin + url.pathname).toBe(
      'https://kc.example/realms/pivox/protocol/openid-connect/logout',
    );
    expect(url.searchParams.get('post_logout_redirect_uri')).toBe('pivox://signed-out');
    expect(url.searchParams.get('id_token_hint')).toBe('the-id-token');
  });

  it('omits optional params when not supplied', () => {
    const url = buildEndSessionUrl(testConfig());
    expect(url.searchParams.get('post_logout_redirect_uri')).toBeNull();
    expect(url.searchParams.get('id_token_hint')).toBeNull();
  });
});
