import { afterEach, describe, expect, it, vi } from 'vitest';

import { resolveSsoProvider } from '@/shared/resolve-sso-provider';

function mockFetch(status: number, body?: unknown) {
  return vi.fn(
    async () =>
      new Response(body === undefined ? null : JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
      }),
  );
}

describe('resolveSsoProvider', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns the provider id from a 200 response', async () => {
    const fetchMock = mockFetch(200, { provider_id: 'oidc.acme' });
    vi.stubGlobal('fetch', fetchMock);

    const result = await resolveSsoProvider(
      'user@acme.com',
      'https://pivox.test',
    );

    expect(result).toBe('oidc.acme');
    expect(fetchMock).toHaveBeenCalledWith(
      'https://pivox.test/internal/v1/auth:resolveProvider',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ email: 'user@acme.com' }),
      }),
    );
  });

  it('returns null on a 404 (no SSO configured for the domain)', async () => {
    vi.stubGlobal('fetch', mockFetch(404));
    expect(
      await resolveSsoProvider('user@nowhere.com', 'https://pivox.test'),
    ).toBeNull();
  });

  it('returns null when a 200 response carries no provider id', async () => {
    vi.stubGlobal('fetch', mockFetch(200, {}));
    expect(
      await resolveSsoProvider('user@acme.com', 'https://pivox.test'),
    ).toBeNull();
  });

  it('throws on a server error', async () => {
    vi.stubGlobal('fetch', mockFetch(500, {}));
    await expect(
      resolveSsoProvider('user@acme.com', 'https://pivox.test'),
    ).rejects.toThrow();
  });

  it('trims a trailing slash from the base URL', async () => {
    const fetchMock = mockFetch(200, { provider_id: 'oidc.x' });
    vi.stubGlobal('fetch', fetchMock);

    await resolveSsoProvider('user@x.com', 'https://pivox.test/');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://pivox.test/internal/v1/auth:resolveProvider',
      expect.anything(),
    );
  });

  it('propagates a network failure from fetch', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('network down');
      }),
    );
    await expect(
      resolveSsoProvider('user@acme.com', 'https://pivox.test'),
    ).rejects.toThrow('network down');
  });

  it('throws when a 200 response body is not valid JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('<html>not json</html>', { status: 200 })),
    );
    await expect(
      resolveSsoProvider('user@acme.com', 'https://pivox.test'),
    ).rejects.toThrow();
  });
});
