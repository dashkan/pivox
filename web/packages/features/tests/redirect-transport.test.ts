import { describe, expect, it } from 'vitest';

import {
  buildBrokerStartUrl,
  parseBrokerRedirect,
} from '@/shared/redirect-transport';

describe('parseBrokerRedirect', () => {
  it('parses a GitHub success fragment', () => {
    expect(
      parseBrokerRedirect(
        '#provider=github&kind=github_access_token&token=gho_abc',
      ),
    ).toEqual({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 'gho_abc',
    });
  });

  it('parses a Google success fragment with an access token', () => {
    expect(
      parseBrokerRedirect(
        '#provider=google&kind=google_id_token&token=idtok&access_token=at123',
      ),
    ).toEqual({
      ok: true,
      provider: 'google',
      kind: 'google_id_token',
      token: 'idtok',
      accessToken: 'at123',
    });
  });

  it('parses an OIDC success fragment with a nonce', () => {
    expect(
      parseBrokerRedirect(
        '#provider=oidc.acme&kind=oidc_id_token&token=idtok&nonce=n0nce',
      ),
    ).toEqual({
      ok: true,
      provider: 'oidc.acme',
      kind: 'oidc_id_token',
      token: 'idtok',
      nonce: 'n0nce',
    });
  });

  it('omits accessToken and nonce when they are absent', () => {
    expect(
      parseBrokerRedirect('#provider=google&kind=google_id_token&token=idtok'),
    ).toEqual({
      ok: true,
      provider: 'google',
      kind: 'google_id_token',
      token: 'idtok',
    });
  });

  it('parses an error fragment', () => {
    expect(parseBrokerRedirect('#error=access_denied')).toEqual({
      ok: false,
      error: 'access_denied',
    });
  });

  it('parses an error fragment with a description', () => {
    expect(
      parseBrokerRedirect(
        '#error=server_error&error_description=Something+broke',
      ),
    ).toEqual({
      ok: false,
      error: 'server_error',
      errorDescription: 'Something broke',
    });
  });

  it('treats an error fragment as a failure even when a token is also present', () => {
    expect(
      parseBrokerRedirect(
        '#error=access_denied&kind=google_id_token&token=idtok',
      ),
    ).toEqual({ ok: false, error: 'access_denied' });
  });

  it('accepts a fragment without the leading hash', () => {
    expect(
      parseBrokerRedirect('provider=github&kind=github_access_token&token=t'),
    ).toEqual({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 't',
    });
  });

  it('extracts the fragment from a full URL', () => {
    expect(
      parseBrokerRedirect(
        'http://127.0.0.1:5400/cb?es=xyz#provider=github&kind=github_access_token&token=t',
      ),
    ).toEqual({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 't',
    });
  });

  it('rejects a success fragment missing the token', () => {
    expect(
      parseBrokerRedirect('#provider=google&kind=google_id_token'),
    ).toEqual({ ok: false, error: 'invalid_broker_response' });
  });

  it('rejects a success fragment missing the provider', () => {
    expect(parseBrokerRedirect('#kind=google_id_token&token=t')).toEqual({
      ok: false,
      error: 'invalid_broker_response',
    });
  });

  it('rejects an unknown credential kind', () => {
    expect(parseBrokerRedirect('#provider=x&kind=bogus_kind&token=t')).toEqual({
      ok: false,
      error: 'invalid_broker_response',
    });
  });

  it('rejects an empty fragment', () => {
    expect(parseBrokerRedirect('')).toEqual({
      ok: false,
      error: 'invalid_broker_response',
    });
  });

  it('rejects a fragment that is only a hash', () => {
    expect(parseBrokerRedirect('#')).toEqual({
      ok: false,
      error: 'invalid_broker_response',
    });
  });

  it('takes the first value when a parameter is duplicated', () => {
    expect(
      parseBrokerRedirect(
        '#provider=github&kind=github_access_token&token=first&token=second',
      ),
    ).toEqual({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 'first',
    });
  });

  it('decodes percent-encoded parameter values', () => {
    expect(
      parseBrokerRedirect(
        '#provider=github&kind=github_access_token&token=gho%2Fa%2Bb',
      ),
    ).toEqual({
      ok: true,
      provider: 'github',
      kind: 'github_access_token',
      token: 'gho/a+b',
    });
  });

  it('rejects a fragment with an empty error value', () => {
    expect(parseBrokerRedirect('#error=')).toEqual({
      ok: false,
      error: 'invalid_broker_response',
    });
  });
});

describe('buildBrokerStartUrl', () => {
  it('builds the broker start URL for a static provider', () => {
    const url = new URL(
      buildBrokerStartUrl({
        baseUrl: 'https://pivox.test',
        provider: 'github',
        returnUrl: 'pivox://auth-complete?es=abc',
      }),
    );
    expect(url.origin).toBe('https://pivox.test');
    expect(url.pathname).toBe('/internal/v1/auth/github/start');
    expect(url.searchParams.get('return')).toBe('pivox://auth-complete?es=abc');
    expect(url.searchParams.has('login_hint')).toBe(false);
  });

  it('includes login_hint when provided', () => {
    const url = new URL(
      buildBrokerStartUrl({
        baseUrl: 'https://pivox.test',
        provider: 'github',
        returnUrl: 'pivox://auth-complete',
        loginHint: 'user@example.com',
      }),
    );
    expect(url.searchParams.get('login_hint')).toBe('user@example.com');
  });

  it('places an OIDC provider id in the path', () => {
    const url = new URL(
      buildBrokerStartUrl({
        baseUrl: 'https://pivox.test',
        provider: 'oidc.acme',
        returnUrl: 'pivox://auth-complete',
      }),
    );
    expect(url.pathname).toBe('/internal/v1/auth/oidc.acme/start');
  });

  it('trims a trailing slash from the base URL', () => {
    const url = buildBrokerStartUrl({
      baseUrl: 'https://pivox.test/',
      provider: 'github',
      returnUrl: 'pivox://auth-complete',
    });
    expect(
      url.startsWith('https://pivox.test/internal/v1/auth/github/start'),
    ).toBe(true);
  });

  it('percent-encodes the return URL so its delimiters do not leak', () => {
    const url = buildBrokerStartUrl({
      baseUrl: 'https://pivox.test',
      provider: 'github',
      returnUrl: 'http://127.0.0.1:5400/cb?es=abc',
    });
    expect(url).toContain(
      'return=http%3A%2F%2F127.0.0.1%3A5400%2Fcb%3Fes%3Dabc',
    );
  });
});
