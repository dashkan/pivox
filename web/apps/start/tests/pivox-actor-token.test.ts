import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';

import {
  createActorTokenSource,
  createGcpActorTokenMint,
  type ActorTokenMint,
} from '../src/server/pivox-actor-token';

const HOUR_MS = 60 * 60 * 1000;

/**
 * makeMint builds a vi.fn-backed ActorTokenMint that returns a
 * deterministic token + expiry per uid. Test assertions read
 * `mint.mock.calls.length` for invocation counts.
 */
function makeMint(
  opts: { lifetimeMs?: number } = {},
): Mock<ActorTokenMint> {
  return vi.fn<ActorTokenMint>((uid) =>
    Promise.resolve({
      token: `token-for-${uid}`,
      expiresAt: Date.now() + (opts.lifetimeMs ?? HOUR_MS),
    }),
  );
}

describe('createActorTokenSource', () => {
  // Fake timers pin Date.now() so the cache's headroom-window
  // comparisons are deterministic across CI latency. The mints
  // produce expiry = now + lifetime; the cache reads now + headroom.
  // Without fake timers the relationships still hold (Date.now()
  // advances by microseconds between writes/reads) but pinning
  // makes the boundaries crisp.
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('mints on first call and returns the signed token', async () => {
    const mint = makeMint();
    const getToken = createActorTokenSource(mint);

    const got = await getToken('user-1');

    expect(got).toBe('token-for-user-1');
    expect(mint).toHaveBeenCalledTimes(1);
  });

  it('reuses the cached token on second call within lifetime', async () => {
    const mint = makeMint();
    const getToken = createActorTokenSource(mint);

    const first = await getToken('user-1');
    const second = await getToken('user-1');

    expect(first).toBe(second);
    expect(mint).toHaveBeenCalledTimes(1);
  });

  it('mints separately per uid', async () => {
    const mint = makeMint();
    const getToken = createActorTokenSource(mint);

    const a = await getToken('user-a');
    const b = await getToken('user-b');

    expect(a).not.toBe(b);
    expect(mint).toHaveBeenCalledTimes(2);
  });

  it('dedupes concurrent first-mints for the same uid', async () => {
    // Two simultaneous callers for the same uid during the very
    // first mint should NOT trigger two signJwt round-trips. They
    // share one in-flight Promise.
    let resolveMint: ((value: { token: string; expiresAt: number }) => void) | undefined;
    const mint = vi.fn<ActorTokenMint>(
      () =>
        new Promise<{ token: string; expiresAt: number }>((r) => {
          resolveMint = r;
        }),
    );
    const getToken = createActorTokenSource(mint);

    const p1 = getToken('user-1');
    const p2 = getToken('user-1');

    expect(mint).toHaveBeenCalledTimes(1);

    if (!resolveMint) throw new Error('mint never registered its resolver');
    resolveMint({ token: 'shared', expiresAt: Date.now() + HOUR_MS });
    const [a, b] = await Promise.all([p1, p2]);

    expect(a).toBe('shared');
    expect(b).toBe('shared');
    expect(mint).toHaveBeenCalledTimes(1);
  });

  it('re-mints when the cached token is within the refresh-headroom window', async () => {
    // The cache layer re-mints when there's < 60s left on the
    // cached token, so a token already inside that window forces a
    // fresh call.
    const mint = makeMint({ lifetimeMs: 30_000 }); // below the 60s headroom
    const getToken = createActorTokenSource(mint);

    await getToken('user-1');
    await getToken('user-1');

    expect(mint).toHaveBeenCalledTimes(2);
  });

  it('does not pin a rejected promise after a failed mint', async () => {
    // A mint error must clear the in-flight entry so the next
    // caller gets a fresh attempt — otherwise a transient signJwt
    // failure would lock that uid out for the process lifetime.
    let attempt = 0;
    const mint = vi.fn<ActorTokenMint>((uid) => {
      attempt++;
      if (attempt === 1) return Promise.reject(new Error('transient'));
      return Promise.resolve({
        token: `recovered-${uid}`,
        expiresAt: Date.now() + HOUR_MS,
      });
    });
    const getToken = createActorTokenSource(mint);

    await expect(getToken('user-1')).rejects.toThrow('transient');

    const ok = await getToken('user-1');
    expect(ok).toBe('recovered-user-1');
    expect(mint).toHaveBeenCalledTimes(2);
  });

  it('returns different cached tokens for different uids under concurrent load', async () => {
    // Stress check that the in-flight map is keyed by uid, not
    // shared across all callers. Concurrent mints for distinct
    // uids must produce distinct tokens.
    const mint = makeMint();
    const getToken = createActorTokenSource(mint);

    const [a, b, c] = await Promise.all([
      getToken('user-a'),
      getToken('user-b'),
      getToken('user-c'),
    ]);

    expect(new Set([a, b, c]).size).toBe(3);
    expect(mint).toHaveBeenCalledTimes(3);
  });
});

describe('createGcpActorTokenMint', () => {
  // The production minter is a thin wrapper around iamcredentials.
  // We can't unit-test the actual signJwt call without GCP creds,
  // but the synchronous validation guards ARE worth pinning — they
  // fail fast at construction time on misconfiguration, which is
  // the most operationally useful error path.
  it('throws synchronously on missing serviceAccountEmail', () => {
    expect(() =>
      createGcpActorTokenMint({ serviceAccountEmail: '', audience: 'aud' }),
    ).toThrow('serviceAccountEmail is required');
  });

  it('throws synchronously on missing audience', () => {
    expect(() =>
      createGcpActorTokenMint({
        serviceAccountEmail: 'ssr@pivox.iam.gserviceaccount.com',
        audience: '',
      }),
    ).toThrow('audience is required');
  });
});
