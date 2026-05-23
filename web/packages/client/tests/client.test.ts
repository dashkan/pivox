import { describe, expect, it, vi } from 'vitest';

import { createApiClient } from '@/client';

/**
 * Build a fetch stub that records every request and returns a fixed
 * JSON body. openapi-fetch awaits Response.json(), so we always return
 * a parseable body.
 */
function recordingFetch(body: unknown = {}): {
  fetch: typeof globalThis.fetch;
  calls: Request[];
} {
  const calls: Request[] = [];
  const fetch: typeof globalThis.fetch = (input, init) => {
    const request =
      input instanceof Request ? input : new Request(input, init);
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

// Real spec path with a real path parameter. Exercising a real path is
// load-bearing: openapi-fetch silently substitutes unknown placeholders
// at runtime, so calling a fabricated path masks the very type-safety
// this package exists to provide.
const GET_ORG_PATH = '/v1/organizations/{organization}' as const;
const ORG_PATH_PARAMS = {
  params: { path: { organization: 'acme' } },
} as const;

describe('createApiClient', () => {
  it('issues requests against the configured baseUrl', async () => {
    const { fetch, calls } = recordingFetch();
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe('https://api.pivox.test/v1/organizations/acme');
  });

  it('does not set an Authorization header when no token getter is provided', async () => {
    const { fetch, calls } = recordingFetch();
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBeNull();
  });
});

describe('auth middleware', () => {
  it('injects Authorization: Bearer when getAuthToken returns a token', async () => {
    const { fetch, calls } = recordingFetch();
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
      getAuthToken: () => 'fbjwt-abc',
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBe('Bearer fbjwt-abc');
  });

  it('awaits an async token getter', async () => {
    const { fetch, calls } = recordingFetch();
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
      getAuthToken: () => Promise.resolve('async-token'),
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBe('Bearer async-token');
  });

  it('omits the Authorization header when getAuthToken returns null', async () => {
    const { fetch, calls } = recordingFetch();
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
      getAuthToken: () => null,
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(calls[0]?.headers.get('Authorization')).toBeNull();
  });

  it('invokes the token getter once per request (no caching)', async () => {
    const { fetch } = recordingFetch();
    const getAuthToken = vi.fn().mockResolvedValue('rotating-token');
    const client = createApiClient({
      baseUrl: 'https://api.pivox.test',
      fetch,
      getAuthToken,
    });

    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);
    await client.GET(GET_ORG_PATH, ORG_PATH_PARAMS);

    expect(getAuthToken).toHaveBeenCalledTimes(2);
  });
});
