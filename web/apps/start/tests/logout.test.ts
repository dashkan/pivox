import { afterEach, describe, expect, it, vi } from 'vitest';

// We test the /auth/logout POST handler directly — it's exported as a plain
// `handleLogout(ctx) => Promise<Response>`, so there's no router runtime to
// stand up and nothing to cast out of the route object. All side-effecting
// deps (store, OIDC client, end-session URL builder) are mocked so we can
// assert the CSRF guard + revocation behavior. createFileRoute is stubbed only
// to keep the module's route construction inert at import time.
vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (opts: unknown) => opts,
}));

const { readSessionIdMock } = vi.hoisted(() => ({ readSessionIdMock: vi.fn() }));
const { getSessionMock, deleteSessionMock } = vi.hoisted(() => ({
  getSessionMock: vi.fn(),
  deleteSessionMock: vi.fn(),
}));
const { getOidcConfigMock, publicOriginMock } = vi.hoisted(() => ({
  getOidcConfigMock: vi.fn(),
  publicOriginMock: vi.fn(),
}));
const { buildEndSessionUrlMock } = vi.hoisted(() => ({ buildEndSessionUrlMock: vi.fn() }));

vi.mock('@/server/oidc/session', () => ({
  readSessionId: readSessionIdMock,
  sessionClearCookie: () => '__pivox_oidc=; Path=/; Max-Age=0',
}));
vi.mock('@/server/oidc/session-store', () => ({
  getSession: getSessionMock,
  deleteSession: deleteSessionMock,
}));
vi.mock('@/server/oidc/client', () => ({
  getOidcConfig: getOidcConfigMock,
  publicOrigin: publicOriginMock,
}));
vi.mock('openid-client', () => ({
  buildEndSessionUrl: buildEndSessionUrlMock,
}));
vi.mock('@pivox/storage', () => ({ ACTIVE_ORG: { name: 'active_org', path: '/' } }));

import { handleLogout as handler } from '../src/routes/auth/logout';

function logoutRequest(headers: Record<string, string>): Request {
  return new Request('http://localhost:3000/auth/logout', { method: 'POST', headers });
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('POST /auth/logout', () => {
  it('rejects a cross-site request with 403 and never deletes the session', async () => {
    // The CSRF case the fix exists for: a cross-site POST must NOT force-logout.
    readSessionIdMock.mockReturnValue('sid-1');

    const res = await handler({
      request: logoutRequest({ origin: 'https://evil.example', 'sec-fetch-site': 'cross-site' }),
    });

    expect(res.status).toBe(403);
    expect(deleteSessionMock).not.toHaveBeenCalled();
  });

  it('proceeds on a same-origin request: deletes the row, clears cookies, redirects to end-session', async () => {
    readSessionIdMock.mockReturnValue('sid-1');
    getSessionMock.mockResolvedValue({ id_token: 'id-token-1' });
    publicOriginMock.mockReturnValue('https://app.example');
    getOidcConfigMock.mockResolvedValue({});
    buildEndSessionUrlMock.mockReturnValue(new URL('https://kc.example/realms/pivox/logout'));

    const res = await handler({ request: logoutRequest({ 'sec-fetch-site': 'same-origin' }) });

    expect(res.status).toBe(302);
    expect(deleteSessionMock).toHaveBeenCalledWith('sid-1');
    // The deleted session's id_token is forwarded as the end-session hint so KC
    // can terminate the IdP session for this exact login.
    expect(buildEndSessionUrlMock).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ id_token_hint: 'id-token-1' }),
    );
    expect(res.headers.get('location')).toBe('https://kc.example/realms/pivox/logout');
    const setCookies = res.headers.getSetCookie();
    expect(setCookies.some((c) => c.startsWith('__pivox_oidc='))).toBe(true);
    expect(setCookies.some((c) => c.startsWith('active_org='))).toBe(true);
  });

  it('accepts a same-origin request matched by the Origin header alone', async () => {
    readSessionIdMock.mockReturnValue(undefined); // no live session: logout is idempotent
    publicOriginMock.mockReturnValue('http://localhost:3000');
    getOidcConfigMock.mockResolvedValue({});
    buildEndSessionUrlMock.mockReturnValue(new URL('http://localhost:3000/'));

    const res = await handler({
      request: logoutRequest({ origin: 'http://localhost:3000' }),
    });

    expect(res.status).toBe(302);
    expect(deleteSessionMock).not.toHaveBeenCalled();
  });
});
