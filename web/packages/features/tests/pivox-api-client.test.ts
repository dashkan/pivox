import { afterEach, describe, expect, it, vi } from 'vitest';

import { createPivoxApiClient } from '@/shared/pivox-api-client';

/**
 * createPivoxApiClient wraps @pivox/client. The web BFF supplies no token (the
 * proxy attaches it from an HttpOnly cookie); Electron has no BFF, so it must be
 * able to source a Keycloak access token via getAuthToken. These tests exercise
 * the real openapi-fetch client and assert the outgoing Authorization header,
 * stubbing globalThis.fetch (the client resolves fetch at call time).
 */
const GET_ORG_PATH = '/v1/organizations/{organization}' as const;
const ORG_PATH_PARAMS = { params: { path: { organization: 'acme' } } } as const;

function recordingFetch(body: unknown = {}): {
  fetch: typeof globalThis.fetch;
  calls: Request[];
} {
  const calls: Request[] = [];
  const fetch: typeof globalThis.fetch = (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    calls.push(request);
    return Promise.resolve(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
  };
  return { fetch, calls };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('createPivoxApiClient', () => {
  it('forwards getAuthToken so calls carry Authorization: Bearer', async () => {
    const { fetch, calls } = recordingFetch();
    vi.stubGlobal('fetch', fetch);

    const client = createPivoxApiClient({
      baseUrl: 'https://api.pivox.test',
      getAuthToken: () => 'kc-access-token',
    });
    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBe('Bearer kc-access-token');
  });

  it('awaits an async token getter (transparent refresh)', async () => {
    const { fetch, calls } = recordingFetch();
    vi.stubGlobal('fetch', fetch);

    const client = createPivoxApiClient({
      baseUrl: 'https://api.pivox.test',
      getAuthToken: async () => 'kc-refreshed',
    });
    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBe('Bearer kc-refreshed');
  });

  it('omits Authorization when no token getter is supplied (BFF-cookie mode)', async () => {
    const { fetch, calls } = recordingFetch();
    vi.stubGlobal('fetch', fetch);

    const client = createPivoxApiClient({ baseUrl: 'https://api.pivox.test' });
    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBeNull();
  });
});
