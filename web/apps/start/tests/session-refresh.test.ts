import { afterEach, describe, expect, it, vi } from 'vitest';

// Direct coverage for refreshSession's security-critical internals: it re-reads
// the row INSIDE the single flight (so it spends the row's CURRENT refresh token,
// never a caller-captured one that a prior flight already rotated), short-circuits
// when the re-read row is already fresh (no second spend → no Keycloak reuse
// detection → no token-family revocation), single-flights concurrent callers, and
// fails closed when there's no row / no refresh token. We mock the store, the
// OIDC config, and the token-grant call; tokensFromResponse runs for real.
const { getSessionMock, updateSessionMock } = vi.hoisted(() => ({
  getSessionMock: vi.fn(),
  updateSessionMock: vi.fn(),
}));
const { getOidcConfigMock } = vi.hoisted(() => ({ getOidcConfigMock: vi.fn() }));
const { refreshTokenGrantMock } = vi.hoisted(() => ({ refreshTokenGrantMock: vi.fn() }));

vi.mock('@/server/oidc/client', () => ({
  getOidcConfig: getOidcConfigMock,
}));
vi.mock('@/server/oidc/session-store', () => ({
  getSession: getSessionMock,
  updateSession: updateSessionMock,
}));
vi.mock('openid-client', () => ({
  refreshTokenGrant: refreshTokenGrantMock,
}));

import { refreshSession } from '../src/server/oidc/session';

const SKEW = 30_000;

afterEach(() => {
  vi.clearAllMocks();
});

describe('refreshSession', () => {
  it('refreshes a near-expiry row, spending the row\'s current refresh token, and persists once', async () => {
    getSessionMock.mockResolvedValue({
      access_token: 'stale',
      refresh_token: 'rt-current',
      expires_at: Date.now() + 5_000, // inside skew
    });
    getOidcConfigMock.mockResolvedValue({ cfg: true });
    refreshTokenGrantMock.mockResolvedValue({
      access_token: 'fresh',
      refresh_token: 'rt-rotated',
      id_token: 'idt',
      expires_in: 300,
    });

    const result = await refreshSession('sid-1');

    // Spends the CURRENT refresh token read from the row, not any caller value.
    expect(refreshTokenGrantMock).toHaveBeenCalledTimes(1);
    expect(refreshTokenGrantMock).toHaveBeenCalledWith({ cfg: true }, 'rt-current');
    expect(updateSessionMock).toHaveBeenCalledTimes(1);
    expect(updateSessionMock).toHaveBeenCalledWith('sid-1', expect.objectContaining({ access_token: 'fresh' }));
    expect(result.access_token).toBe('fresh');
    expect(result.refresh_token).toBe('rt-rotated');
    expect(result.expires_at).toBeGreaterThan(Date.now() + SKEW);
  });

  it('short-circuits when the re-read row is already fresh — no second token spend', async () => {
    // The sequential-reuse case: a prior flight already rotated + persisted, so
    // the row now holds a still-valid access token. We must return it, NOT spend
    // the (now-rotated, stale) refresh token again.
    getSessionMock.mockResolvedValue({
      access_token: 'already-fresh',
      refresh_token: 'rt-current',
      expires_at: Date.now() + 5 * 60_000, // well past skew
    });

    const result = await refreshSession('sid-1');

    expect(refreshTokenGrantMock).not.toHaveBeenCalled();
    expect(updateSessionMock).not.toHaveBeenCalled();
    expect(result.access_token).toBe('already-fresh');
  });

  it('single-flights concurrent callers on the same id: one re-read, one grant, one write', async () => {
    getSessionMock.mockResolvedValue({
      access_token: 'stale',
      refresh_token: 'rt-current',
      expires_at: Date.now() + 5_000,
    });
    getOidcConfigMock.mockResolvedValue({});
    refreshTokenGrantMock.mockResolvedValue({
      access_token: 'fresh',
      refresh_token: 'rt-rotated',
      expires_in: 300,
    });

    const [a, b] = await Promise.all([refreshSession('sid-1'), refreshSession('sid-1')]);

    expect(refreshTokenGrantMock).toHaveBeenCalledTimes(1);
    expect(updateSessionMock).toHaveBeenCalledTimes(1);
    expect(a).toEqual(b);
    expect(a.access_token).toBe('fresh');
  });

  it('keeps the old refresh token when the IdP does not rotate (no refresh_token in the grant)', async () => {
    getSessionMock.mockResolvedValue({
      access_token: 'stale',
      refresh_token: 'rt-current',
      expires_at: Date.now() + 5_000,
    });
    getOidcConfigMock.mockResolvedValue({});
    refreshTokenGrantMock.mockResolvedValue({ access_token: 'fresh', expires_in: 300 }); // no refresh_token

    const result = await refreshSession('sid-1');

    expect(result.refresh_token).toBe('rt-current');
    expect(updateSessionMock).toHaveBeenCalledWith('sid-1', expect.objectContaining({ refresh_token: 'rt-current' }));
  });

  it('fails closed when the row is gone', async () => {
    getSessionMock.mockResolvedValue(undefined);
    await expect(refreshSession('sid-1')).rejects.toThrow();
    expect(refreshTokenGrantMock).not.toHaveBeenCalled();
  });

  it('fails closed when the near-expiry row has no refresh token', async () => {
    getSessionMock.mockResolvedValue({ access_token: 'stale', expires_at: Date.now() + 5_000 });
    await expect(refreshSession('sid-1')).rejects.toThrow();
    expect(refreshTokenGrantMock).not.toHaveBeenCalled();
  });
});
