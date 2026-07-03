import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AuthSession, type TokenPersistence } from '../src/main/auth-session';

import type { SessionTokens } from '@pivox/oidc';

// AuthSession is the electron main-process token engine: it holds the access
// token in memory, persists the refresh token (via an injected TokenPersistence
// so safeStorage stays out of the unit), single-flights refresh so a burst of
// getAccessToken() callers can't double-spend a rotating refresh token, and
// restores a session on boot from the persisted refresh token. All electron
// coupling (safeStorage, id-token decode) is injected, so this runs under Node.
//
// We mock @pivox/oidc's refreshTokens (the token-grant boundary — its own
// behavior, including rotation fallback, is covered by that package's tests) but
// keep the REAL isTokenFresh so the freshness gating is exercised for real.
const { refreshTokensMock } = vi.hoisted(() => ({
  refreshTokensMock: vi.fn(),
}));

vi.mock('@pivox/oidc', async (importActual) => {
  const actual = await importActual<typeof import('@pivox/oidc')>();
  return { ...actual, refreshTokens: refreshTokensMock };
});

/** In-memory TokenPersistence double so tests can assert save/clear. */
function fakePersistence(initial: string | null = null): TokenPersistence & {
  value: string | null;
} {
  const store = { value: initial };
  return {
    get value() {
      return store.value;
    },
    load: () => store.value,
    save: (rt: string) => {
      store.value = rt;
    },
    clear: () => {
      store.value = null;
    },
  };
}

const CONFIG = { cfg: true } as never;
const configProvider = () => Promise.resolve(CONFIG);

// A minimal id_token whose claims our injected decoder returns. Its actual bytes
// don't matter — decodeIdToken is stubbed.
function tokenSet(overrides: Partial<SessionTokens> = {}): SessionTokens {
  return {
    access_token: 'at',
    refresh_token: 'rt',
    id_token: 'idt',
    expires_at: Date.now() + 5 * 60_000,
    ...overrides,
  };
}

const decodeIdToken = vi.fn((_idToken: string) => ({
  sub: 'user-123',
  email: 'user@acme.test',
  name: 'Ada Lovelace',
  picture: 'https://cdn/pic.png',
}));

function makeSession(persistence: TokenPersistence) {
  return new AuthSession({
    config: configProvider,
    persistence,
    decodeIdToken,
    scope: 'openid profile email offline_access',
  });
}

beforeEach(() => {
  decodeIdToken.mockClear();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('AuthSession.setTokens / getUser', () => {
  it('holds tokens, persists the refresh token, and derives the user from the id_token', () => {
    const persistence = fakePersistence();
    const session = makeSession(persistence);

    session.setTokens(tokenSet());

    expect(session.isAuthenticated()).toBe(true);
    expect(persistence.value).toBe('rt');
    expect(session.getUser()).toEqual({
      id: 'user-123',
      email: 'user@acme.test',
      displayName: 'Ada Lovelace',
      photoURL: 'https://cdn/pic.png',
    });
  });

  it('reports no user when there is no id_token', () => {
    const session = makeSession(fakePersistence());
    session.setTokens(tokenSet({ id_token: undefined }));
    expect(session.getUser()).toBeNull();
  });
});

describe('AuthSession.getAccessToken', () => {
  it('returns the in-memory token without refreshing when it is fresh', async () => {
    const session = makeSession(fakePersistence());
    session.setTokens(tokenSet({ access_token: 'fresh-at', expires_at: Date.now() + 5 * 60_000 }));

    expect(await session.getAccessToken()).toBe('fresh-at');
    expect(refreshTokensMock).not.toHaveBeenCalled();
  });

  it('refreshes a near-expiry token and persists the rotated refresh token', async () => {
    const persistence = fakePersistence();
    const session = makeSession(persistence);
    session.setTokens(tokenSet({ access_token: 'stale-at', refresh_token: 'rt-1', expires_at: Date.now() + 5_000 }));

    refreshTokensMock.mockResolvedValue({
      access_token: 'rotated-at',
      refresh_token: 'rt-2',
      id_token: 'idt2',
      expires_at: Date.now() + 300_000,
    });

    expect(await session.getAccessToken()).toBe('rotated-at');
    expect(refreshTokensMock).toHaveBeenCalledTimes(1);
    expect(refreshTokensMock).toHaveBeenCalledWith(CONFIG, 'rt-1');
    expect(persistence.value).toBe('rt-2');
  });

  it('single-flights concurrent refreshes: one grant, one persist', async () => {
    const persistence = fakePersistence();
    const session = makeSession(persistence);
    session.setTokens(tokenSet({ refresh_token: 'rt-1', expires_at: Date.now() + 5_000 }));

    refreshTokensMock.mockResolvedValue({
      access_token: 'rotated-at',
      refresh_token: 'rt-2',
      expires_at: Date.now() + 300_000,
    });

    const [a, b] = await Promise.all([session.getAccessToken(), session.getAccessToken()]);

    expect(a).toBe('rotated-at');
    expect(b).toBe('rotated-at');
    expect(refreshTokensMock).toHaveBeenCalledTimes(1);
  });

  it('throws when signed out (no token, no persisted refresh)', async () => {
    const session = makeSession(fakePersistence());
    await expect(session.getAccessToken()).rejects.toThrow();
    expect(refreshTokensMock).not.toHaveBeenCalled();
  });

  it('clears the session when a refresh fails, so it transitions to signed-out (not a 401 zombie)', async () => {
    const persistence = fakePersistence();
    const session = makeSession(persistence);
    session.setTokens(tokenSet({ refresh_token: 'rt-dead', expires_at: Date.now() + 5_000 }));

    refreshTokensMock.mockRejectedValue(new Error('invalid_grant'));

    await expect(session.getAccessToken()).rejects.toThrow('invalid_grant');
    expect(session.isAuthenticated()).toBe(false);
    expect(session.getUser()).toBeNull();
    expect(persistence.value).toBeNull();
  });
});

describe('AuthSession.restore', () => {
  it('restores a session on boot from the persisted refresh token', async () => {
    const persistence = fakePersistence('rt-persisted');
    const session = makeSession(persistence);

    refreshTokensMock.mockResolvedValue({
      access_token: 'restored-at',
      refresh_token: 'rt-next',
      id_token: 'idt',
      expires_at: Date.now() + 300_000,
    });

    const restored = await session.restore();

    expect(restored).toBe(true);
    expect(session.isAuthenticated()).toBe(true);
    expect(refreshTokensMock).toHaveBeenCalledWith(CONFIG, 'rt-persisted');
    expect(await session.getAccessToken()).toBe('restored-at');
    expect(persistence.value).toBe('rt-next');
  });

  it('returns false and clears persistence when the stored refresh token is dead', async () => {
    const persistence = fakePersistence('rt-dead');
    const session = makeSession(persistence);

    refreshTokensMock.mockRejectedValue(new Error('invalid_grant'));

    const restored = await session.restore();

    expect(restored).toBe(false);
    expect(session.isAuthenticated()).toBe(false);
    expect(persistence.value).toBeNull();
  });

  it('returns false without a grant when nothing is persisted', async () => {
    const session = makeSession(fakePersistence(null));
    expect(await session.restore()).toBe(false);
    expect(refreshTokensMock).not.toHaveBeenCalled();
  });
});

describe('AuthSession.clear', () => {
  it('wipes in-memory tokens and persisted refresh token', () => {
    const persistence = fakePersistence();
    const session = makeSession(persistence);
    session.setTokens(tokenSet());

    session.clear();

    expect(session.isAuthenticated()).toBe(false);
    expect(session.getUser()).toBeNull();
    expect(persistence.value).toBeNull();
  });
});
