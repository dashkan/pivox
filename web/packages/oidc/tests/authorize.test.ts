import * as oidc from 'openid-client';
import { describe, expect, it } from 'vitest';


import { buildAuthorizationRequest } from '@/authorize';

// buildAuthorizationRequest runs the real PKCE helpers + buildAuthorizationUrl
// against a network-free Configuration, so we can assert the exact query the
// browser is sent to Keycloak with.
function testConfig(): oidc.Configuration {
  return new oidc.Configuration(
    {
      issuer: 'https://kc.example/realms/pivox',
      authorization_endpoint: 'https://kc.example/realms/pivox/protocol/openid-connect/auth',
      token_endpoint: 'https://kc.example/realms/pivox/protocol/openid-connect/token',
    },
    'electron',
    undefined,
    oidc.None(),
  );
}

describe('buildAuthorizationRequest', () => {
  it('builds a PKCE S256 authorization URL and returns the verifier + state', async () => {
    const req = await buildAuthorizationRequest(testConfig(), {
      redirectUri: 'http://127.0.0.1:53127/oidc/callback',
      scope: 'openid profile email offline_access',
    });

    const url = new URL(req.authorizationUrl);
    expect(url.origin + url.pathname).toBe(
      'https://kc.example/realms/pivox/protocol/openid-connect/auth',
    );
    expect(url.searchParams.get('client_id')).toBe('electron');
    expect(url.searchParams.get('redirect_uri')).toBe('http://127.0.0.1:53127/oidc/callback');
    expect(url.searchParams.get('scope')).toBe('openid profile email offline_access');
    expect(url.searchParams.get('response_type')).toBe('code');
    expect(url.searchParams.get('code_challenge_method')).toBe('S256');
    expect(url.searchParams.get('code_challenge')).toBeTruthy();
    // The state in the URL is exactly the one returned for the caller to verify.
    expect(url.searchParams.get('state')).toBe(req.state);

    expect(req.codeVerifier).toMatch(/^[A-Za-z0-9\-._~]{43,128}$/);
    expect(req.state.length).toBeGreaterThan(0);
  });

  it('generates a fresh verifier + state per call', async () => {
    const config = testConfig();
    const a = await buildAuthorizationRequest(config, { redirectUri: 'http://127.0.0.1:1/cb', scope: 'openid' });
    const b = await buildAuthorizationRequest(config, { redirectUri: 'http://127.0.0.1:1/cb', scope: 'openid' });
    expect(a.codeVerifier).not.toBe(b.codeVerifier);
    expect(a.state).not.toBe(b.state);
  });

  it('merges extra params (e.g. login_hint) into the URL', async () => {
    const req = await buildAuthorizationRequest(testConfig(), {
      redirectUri: 'http://127.0.0.1:1/cb',
      scope: 'openid',
      extraParams: { login_hint: 'user@acme.test' },
    });
    expect(new URL(req.authorizationUrl).searchParams.get('login_hint')).toBe('user@acme.test');
  });

  it('never lets extraParams clobber protocol-critical params', async () => {
    // A caller must not be able to override the generated state / PKCE method:
    // the function still returns the GENERATED state, so an overridden URL state
    // would silently fail the callback's expectedState check.
    const req = await buildAuthorizationRequest(testConfig(), {
      redirectUri: 'http://127.0.0.1:1/cb',
      scope: 'openid',
      extraParams: { state: 'attacker', code_challenge_method: 'plain' },
    });
    const url = new URL(req.authorizationUrl);
    expect(url.searchParams.get('state')).toBe(req.state);
    expect(url.searchParams.get('state')).not.toBe('attacker');
    expect(url.searchParams.get('code_challenge_method')).toBe('S256');
  });
});
