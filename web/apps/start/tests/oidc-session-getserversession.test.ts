import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// `getServerSession` wraps its body in `createServerFn(...).handler(fn)`; that
// body is exported as the plain `readServerSession()` for testing, so we call it
// directly and assert branch behavior — no cast out of the wrapped server fn.
// The runtime inputs are the cookie (the opaque session id) and the store
// lookup that resolves it to the token set — both mocked per-case. createServerFn
// is stubbed only to keep the module's server-fn construction inert at import.
const { getCookieMock } = vi.hoisted(() => ({ getCookieMock: vi.fn() }));
const { getSessionMock } = vi.hoisted(() => ({ getSessionMock: vi.fn() }));

vi.mock('@tanstack/react-start', () => ({
  createServerFn: () => ({ handler: (fn: unknown) => fn }),
}));

vi.mock('@tanstack/react-start/server', () => ({
  getCookie: getCookieMock,
}));

vi.mock('@/server/oidc/session-store', () => ({
  getSession: getSessionMock,
}));

import { readServerSession } from '../src/server/oidc-session.server';

/** Build an unsigned-but-well-formed JWT (header.payload.sig). */
function makeIdToken(claims: Record<string, unknown>): string {
  const seg = (o: unknown) =>
    Buffer.from(JSON.stringify(o)).toString('base64url');
  return `${seg({ alg: 'RS256', typ: 'JWT' })}.${seg(claims)}.signature`;
}

const call = () => readServerSession();

const ISSUER = 'https://idp.example/realms/acme';

beforeEach(() => {
  vi.stubEnv('PIVOX_OIDC_ISSUER', ISSUER);
});

afterEach(() => {
  vi.unstubAllEnvs();
  getCookieMock.mockReset();
  getSessionMock.mockReset();
});

describe('getServerSession', () => {
  it('reports no cookie and never hits the store when the cookie is absent', async () => {
    getCookieMock.mockReturnValue(undefined);

    expect(await call()).toEqual({
      user: null,
      cookiePresent: false,
      accountConsoleUrl: `${ISSUER}/account`,
    });
    expect(getSessionMock).not.toHaveBeenCalled();
  });

  it('reports cookiePresent but no user when the id resolves to no session', async () => {
    getCookieMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue(undefined);

    expect(await call()).toEqual({
      user: null,
      cookiePresent: true,
      accountConsoleUrl: `${ISSUER}/account`,
    });
    expect(getSessionMock).toHaveBeenCalledWith('sid-1');
  });

  it('reports no user when the session has no id_token', async () => {
    getCookieMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({ access_token: 'a', expires_at: 0 });

    expect(await call()).toEqual({
      user: null,
      cookiePresent: true,
      accountConsoleUrl: `${ISSUER}/account`,
    });
  });

  it('reports no user when the id_token decodes but carries no sub', async () => {
    getCookieMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'a',
      expires_at: 0,
      id_token: makeIdToken({ email: 'ada@example.com' }),
    });

    expect(await call()).toEqual({
      user: null,
      cookiePresent: true,
      accountConsoleUrl: `${ISSUER}/account`,
    });
  });

  it('returns the user when the id_token carries a sub', async () => {
    getCookieMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({
      access_token: 'a',
      expires_at: 0,
      id_token: makeIdToken({
        sub: 'kc-sub-123',
        email: 'ada@example.com',
        name: 'Ada Lovelace',
        picture: 'https://example.com/p.png',
      }),
    });

    expect(await call()).toEqual({
      user: {
        id: 'kc-sub-123',
        email: 'ada@example.com',
        displayName: 'Ada Lovelace',
        photoURL: 'https://example.com/p.png',
      },
      cookiePresent: true,
      accountConsoleUrl: `${ISSUER}/account`,
    });
  });

  it('nulls accountConsoleUrl when PIVOX_OIDC_ISSUER is unset', async () => {
    vi.stubEnv('PIVOX_OIDC_ISSUER', undefined);
    getCookieMock.mockReturnValue(undefined);

    expect((await call()).accountConsoleUrl).toBeNull();
  });
});
