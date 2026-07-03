import { afterEach, describe, expect, it, vi } from 'vitest';

// createConfigProvider is the riskiest piece: a two-layer memo with
// retry-on-failure. A silent regression here would only surface as a process
// permanently wedged after a transient IdP-down at first login. We mock
// openid-client's discovery + None so we can assert the memo, the retry, and
// which client-auth branch fires (public vs confidential).
const { discoveryMock, noneMock } = vi.hoisted(() => ({
  discoveryMock: vi.fn(),
  noneMock: vi.fn(() => ({ __clientAuth: 'None' })),
}));

vi.mock('openid-client', () => ({
  discovery: discoveryMock,
  None: noneMock,
}));

import { createConfigProvider } from '@/config';

afterEach(() => {
  vi.clearAllMocks();
});

const ISSUER = 'https://kc.example/realms/pivox';

describe('createConfigProvider', () => {
  it('memoizes discovery: many provider() calls trigger exactly one discovery', async () => {
    discoveryMock.mockResolvedValue({ cfg: true });
    const provider = createConfigProvider({ issuer: ISSUER, clientId: 'start', clientSecret: 's' });

    const [a, b] = await Promise.all([provider(), provider()]);
    await provider();

    expect(discoveryMock).toHaveBeenCalledTimes(1);
    expect(a).toBe(b);
  });

  it('clears the cache on discovery failure so the next call retries', async () => {
    discoveryMock
      .mockRejectedValueOnce(new Error('idp down'))
      .mockResolvedValueOnce({ cfg: true });
    const provider = createConfigProvider({ issuer: ISSUER, clientId: 'start', clientSecret: 's' });

    await expect(provider()).rejects.toThrow('idp down');
    await expect(provider()).resolves.toEqual({ cfg: true });
    expect(discoveryMock).toHaveBeenCalledTimes(2);
  });

  it('confidential client: passes the secret as the client_secret shorthand, no None()', async () => {
    discoveryMock.mockResolvedValue({});
    const provider = createConfigProvider({ issuer: ISSUER, clientId: 'start', clientSecret: 'the-secret' });
    await provider();

    const args = discoveryMock.mock.calls[0] ?? [];
    expect(args[0]).toBeInstanceOf(URL);
    expect((args[0] as URL).href).toBe(ISSUER);
    expect(args[1]).toBe('start');
    expect(args[2]).toBe('the-secret');
    expect(noneMock).not.toHaveBeenCalled();
  });

  it('public client: omits the secret and selects None() client auth', async () => {
    discoveryMock.mockResolvedValue({});
    const provider = createConfigProvider({ issuer: ISSUER, clientId: 'electron' });
    await provider();

    const args = discoveryMock.mock.calls[0] ?? [];
    expect(args[1]).toBe('electron');
    expect(args[2]).toBeUndefined();
    expect(noneMock).toHaveBeenCalledTimes(1);
    expect(args[3]).toEqual({ __clientAuth: 'None' });
  });

  it('treats an empty-string clientSecret as public (never confidential with an empty secret)', async () => {
    discoveryMock.mockResolvedValue({});
    const provider = createConfigProvider({ issuer: ISSUER, clientId: 'electron', clientSecret: '' });
    await provider();

    const args = discoveryMock.mock.calls[0] ?? [];
    expect(args[2]).toBeUndefined();
    expect(noneMock).toHaveBeenCalledTimes(1);
  });
});
