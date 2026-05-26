import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import {
  _resetServerApiForTests,
  createServerApi,
} from '../src/server/pivox-server-api';

// `google-auth-library` is initialized lazily on first
// createServerApi call. The factory's GoogleAuth construction
// itself is benign (no network), but its iamcredentials calls
// would require live GCP credentials — the validation paths
// these tests exercise fail BEFORE any GCP call, so no creds
// needed.

describe('createServerApi', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    // Reset isolation is via _resetServerApiForTests — the module
    // cache is what holds the singleton config. vi.resetModules()
    // would be a no-op here because the symbols are statically
    // imported at the top of the file (vitest's module registry
    // doesn't touch already-resolved bindings).
    process.env = { ...originalEnv };
    _resetServerApiForTests();
  });

  afterEach(() => {
    process.env = originalEnv;
    _resetServerApiForTests();
  });

  it('throws on missing pivoxUserId', () => {
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    expect(() => createServerApi('')).toThrow(/pivoxUserId is required/);
  });

  it('throws when PIVOX_API_URL is unset', () => {
    delete process.env.PIVOX_API_URL;
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    expect(() => createServerApi('user-1')).toThrow(/PIVOX_API_URL not set/);
  });

  it('throws when PIVOX_SSR_SA_EMAIL is unset', () => {
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    delete process.env.PIVOX_SSR_SA_EMAIL;
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    expect(() => createServerApi('user-1')).toThrow(
      /PIVOX_SSR_SA_EMAIL not set/,
    );
  });

  it('throws when PIVOX_SSR_AUDIENCE is unset', () => {
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    delete process.env.PIVOX_SSR_AUDIENCE;

    expect(() => createServerApi('user-1')).toThrow(
      /PIVOX_SSR_AUDIENCE not set/,
    );
  });

  it('constructs a ReactQueryApi when all env vars are present', () => {
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    const api = createServerApi('user-1');

    // Shape check: openapi-react-query exposes useQuery + queryOptions
    // on the returned object. We don't call them (would require
    // GCP creds + network); just confirm the factory wired up
    // without throwing.
    expect(api).toBeDefined();
    expect(typeof api.queryOptions).toBe('function');
  });

  it('caches env-driven config across createServerApi calls', () => {
    // First call validates all three env vars and constructs the
    // token source + baseUrl. Second call (different uid) must
    // succeed without re-validating — config is cached. Blanking
    // env vars after the first call confirms the cache, not the
    // env, is the source of truth on the second call.
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    createServerApi('user-1');

    delete process.env.PIVOX_API_URL;
    delete process.env.PIVOX_SSR_SA_EMAIL;
    delete process.env.PIVOX_SSR_AUDIENCE;

    expect(() => createServerApi('user-2')).not.toThrow();
  });

  it('returns distinct api objects per uid', () => {
    // Per-uid token caching happens inside the shared
    // ActorTokenSource, but the api object itself is constructed
    // per call (new openapi-fetch client wrapping a uid-bound
    // getAuthToken closure). Distinct references guards against a
    // future refactor that accidentally returns a cached api,
    // which would leak one user's auth into another's calls.
    process.env.PIVOX_API_URL = 'https://api.pivox.app';
    process.env.PIVOX_SSR_SA_EMAIL = 'ssr@pivox.iam.gserviceaccount.com';
    process.env.PIVOX_SSR_AUDIENCE = 'https://api.pivox.app';

    const api1 = createServerApi('user-1');
    const api2 = createServerApi('user-2');

    expect(api1).not.toBe(api2);
  });
});
