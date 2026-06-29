import { afterEach, describe, expect, it, vi } from 'vitest';

// SSR token forwarding resolves the cookie's opaque session id to the stored
// token set and, when near expiry, rotates it via the single-flighted
// refreshSession (which persists the rotated set itself — no cookie write). We
// mock the request surface, the session module, and the store so we can drive
// each branch and assert real behavior (which token is returned, whether a
// refresh happened, that the store is/ isn't consulted).
const { getRequestMock } = vi.hoisted(() => ({ getRequestMock: vi.fn() }));
const { readSessionIdMock, refreshSessionMock } = vi.hoisted(() => ({
  readSessionIdMock: vi.fn(),
  refreshSessionMock: vi.fn(),
}));
const { getSessionMock } = vi.hoisted(() => ({ getSessionMock: vi.fn() }));

vi.mock('@tanstack/react-start/server', () => ({
  getRequest: getRequestMock,
}));

vi.mock('@/server/oidc/session', () => ({
  EXPIRY_SKEW_MS: 30_000,
  readSessionId: readSessionIdMock,
  refreshSession: refreshSessionMock,
}));

vi.mock('@/server/oidc/session-store', () => ({
  getSession: getSessionMock,
}));

import { getSsrAccessToken } from '../src/server/oidc/ssr-token';

const FAKE_REQUEST = { url: 'https://app.example/' } as unknown as Request;

afterEach(() => {
  vi.clearAllMocks();
});

describe('getSsrAccessToken', () => {
  it('returns null and never touches the store when there is no session id', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue(undefined);

    expect(await getSsrAccessToken()).toBeNull();
    expect(getSessionMock).not.toHaveBeenCalled();
    expect(refreshSessionMock).not.toHaveBeenCalled();
  });

  it('returns null when the id resolves to no session', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue(undefined);

    expect(await getSsrAccessToken()).toBeNull();
    expect(getSessionMock).toHaveBeenCalledWith('sid-1');
    expect(refreshSessionMock).not.toHaveBeenCalled();
  });

  it('returns null when the session carries no access token', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({ access_token: '', expires_at: Date.now() + 60_000 });

    expect(await getSsrAccessToken()).toBeNull();
    expect(refreshSessionMock).not.toHaveBeenCalled();
  });

  it('returns the existing token without refreshing when it is not near expiry', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'live-token',
      refresh_token: 'refresh-1',
      expires_at: Date.now() + 5 * 60_000, // well past the 30s skew
    });

    expect(await getSsrAccessToken()).toBe('live-token');
    expect(refreshSessionMock).not.toHaveBeenCalled();
  });

  it('refreshes (single-flighted on the id) and returns the new token near expiry', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'stale-token',
      refresh_token: 'refresh-1',
      expires_at: Date.now() + 5_000, // inside the 30s skew
    });
    refreshSessionMock.mockResolvedValue({
      access_token: 'fresh-token',
      refresh_token: 'refresh-2',
      expires_at: Date.now() + 5 * 60_000,
    });

    const token = await getSsrAccessToken();

    // The NEW token is returned; refreshSession is keyed on the session id and
    // re-reads the current refresh token from the row itself (no token arg), and
    // persists the rotated set itself (no cookie write on this path anymore).
    expect(token).toBe('fresh-token');
    expect(refreshSessionMock).toHaveBeenCalledWith('sid-1');
  });

  it('returns null when the refresh fails', async () => {
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'stale-token',
      refresh_token: 'refresh-1',
      expires_at: Date.now() + 5_000,
    });
    refreshSessionMock.mockRejectedValue(new Error('idp down'));

    expect(await getSsrAccessToken()).toBeNull();
  });

  it('returns the stale token without refreshing when no refresh token is present', async () => {
    // Matches the `&& session.refresh_token` guard: near expiry but nothing to
    // rotate with, so we hand back the (still-valid-ish) access token.
    getRequestMock.mockReturnValue(FAKE_REQUEST);
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'stale-token',
      expires_at: Date.now() + 5_000,
    });

    expect(await getSsrAccessToken()).toBe('stale-token');
    expect(refreshSessionMock).not.toHaveBeenCalled();
  });
});
