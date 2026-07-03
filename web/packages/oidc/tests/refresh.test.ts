import { afterEach, describe, expect, it, vi } from 'vitest';

// refreshTokens is the pure protocol call: refreshTokenGrant (mocked) + mapping
// (real). It keeps the caller's refresh token when the IdP doesn't rotate.
// Single-flight / reuse-detection is the caller's job, not tested here.
const { refreshTokenGrantMock } = vi.hoisted(() => ({
  refreshTokenGrantMock: vi.fn(),
}));

vi.mock('openid-client', () => ({
  refreshTokenGrant: refreshTokenGrantMock,
}));

import { refreshTokens } from '@/refresh';

afterEach(() => {
  vi.clearAllMocks();
});

describe('refreshTokens', () => {
  it('spends the given refresh token and returns the rotated set', async () => {
    refreshTokenGrantMock.mockResolvedValue({
      access_token: 'fresh',
      refresh_token: 'rt-rotated',
      id_token: 'idt',
      expires_in: 300,
    });
    const config = { cfg: true };

    const tokens = await refreshTokens(config as never, 'rt-current');

    expect(refreshTokenGrantMock).toHaveBeenCalledTimes(1);
    expect(refreshTokenGrantMock).toHaveBeenCalledWith(config, 'rt-current');
    expect(tokens.access_token).toBe('fresh');
    expect(tokens.refresh_token).toBe('rt-rotated');
  });

  it('keeps the old refresh token when the IdP does not rotate', async () => {
    refreshTokenGrantMock.mockResolvedValue({ access_token: 'fresh', expires_in: 300 });

    const tokens = await refreshTokens({} as never, 'rt-current');

    expect(tokens.refresh_token).toBe('rt-current');
  });

  it('propagates a refresh failure (e.g. reuse detection / expired) as a rejection', async () => {
    refreshTokenGrantMock.mockRejectedValue(new Error('invalid_grant'));
    await expect(refreshTokens({} as never, 'rt')).rejects.toThrow('invalid_grant');
  });
});
