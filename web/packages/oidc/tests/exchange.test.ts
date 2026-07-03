import { afterEach, describe, expect, it, vi } from 'vitest';

// exchangeAuthorizationCode delegates the code+PKCE exchange to openid-client
// (mocked) and maps the response through tokensFromResponse (real).
const { authorizationCodeGrantMock } = vi.hoisted(() => ({
  authorizationCodeGrantMock: vi.fn(),
}));

vi.mock('openid-client', () => ({
  authorizationCodeGrant: authorizationCodeGrantMock,
}));

import { exchangeAuthorizationCode } from '@/exchange';

afterEach(() => {
  vi.clearAllMocks();
});

describe('exchangeAuthorizationCode', () => {
  it('passes the callback URL + PKCE verifier + expected state and returns mapped tokens', async () => {
    authorizationCodeGrantMock.mockResolvedValue({
      access_token: 'at',
      refresh_token: 'rt',
      id_token: 'idt',
      expires_in: 300,
    });
    const config = { cfg: true };
    const currentUrl = new URL('http://127.0.0.1:1/cb?code=abc&state=xyz');

    const tokens = await exchangeAuthorizationCode(config as never, {
      currentUrl,
      codeVerifier: 'verifier',
      expectedState: 'xyz',
    });

    expect(authorizationCodeGrantMock).toHaveBeenCalledTimes(1);
    expect(authorizationCodeGrantMock).toHaveBeenCalledWith(config, currentUrl, {
      pkceCodeVerifier: 'verifier',
      expectedState: 'xyz',
    });
    expect(tokens.access_token).toBe('at');
    expect(tokens.refresh_token).toBe('rt');
    expect(tokens.id_token).toBe('idt');
    expect(tokens.expires_at).toBeGreaterThan(Date.now());
  });

  it('propagates a state/PKCE mismatch as a rejection', async () => {
    authorizationCodeGrantMock.mockRejectedValue(new Error('state mismatch'));
    await expect(
      exchangeAuthorizationCode({} as never, {
        currentUrl: new URL('http://127.0.0.1:1/cb'),
        codeVerifier: 'v',
        expectedState: 's',
      }),
    ).rejects.toThrow('state mismatch');
  });
});
