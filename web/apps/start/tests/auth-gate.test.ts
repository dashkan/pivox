import { isRedirect } from '@tanstack/react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

// `requireKcSession` reads the BFF session via `getServerSession` (a
// `createServerFn`). We mock the whole module so `getServerSession` is a
// plain `vi.fn` we can drive per-case — the gate's branching is what's
// under test, not the server-fn transport.
const { getServerSessionMock } = vi.hoisted(() => ({
  getServerSessionMock: vi.fn(),
}));

vi.mock('@/server/oidc-session', () => ({
  getServerSession: getServerSessionMock,
}));

import { requireKcSession } from '../src/lib/auth-gate';

const location = { pathname: '/spaces/abc', searchStr: '?tab=assets' };

afterEach(() => {
  vi.unstubAllGlobals();
  getServerSessionMock.mockReset();
});

describe('requireKcSession', () => {
  it('returns the authed session when a user is present', async () => {
    const status = {
      user: {
        id: 'kc-sub-1',
        email: 'ada@example.com',
        displayName: 'Ada',
        photoURL: null,
      },
      cookiePresent: true,
      accountConsoleUrl: 'https://idp.example/realms/acme/account',
    };
    getServerSessionMock.mockResolvedValue(status);

    const result = await requireKcSession(location);

    // The user is carried through verbatim...
    expect(result.user).toEqual(status.user);
    // ...and the status passthrough fields (accountConsoleUrl, cookiePresent)
    // survive the narrowing rebuild `{ ...status, user: status.user }`.
    expect(result.accountConsoleUrl).toBe(status.accountConsoleUrl);
    expect(result.cookiePresent).toBe(true);
  });

  it('throws a reload-document redirect to the sign-in handler on the SSR pass', async () => {
    // vitest's default node env has no `window`, so this exercises the
    // `typeof window === 'undefined'` (SSR) branch.
    expect(typeof window).toBe('undefined');
    getServerSessionMock.mockResolvedValue({
      user: null,
      cookiePresent: false,
      accountConsoleUrl: null,
    });

    const thrown: unknown = await requireKcSession(location).catch(
      (e: unknown) => e,
    );

    // TanStack's `redirect()` returns a throwable; assert it's that shape, not
    // some incidental error.
    expect(isRedirect(thrown)).toBe(true);
    const redirect = thrown as { options: { href: string; reloadDocument: boolean } };
    // Full-document navigation is load-bearing: an SPA redirect would never hit
    // the /auth/sign-in SERVER handler that starts the OAuth flow.
    expect(redirect.options.reloadDocument).toBe(true);
    expect(redirect.options.href).toBe(
      '/auth/sign-in?return=%2Fspaces%2Fabc%3Ftab%3Dassets',
    );
  });

  it('URL-encodes the return path from pathname + searchStr', async () => {
    getServerSessionMock.mockResolvedValue({
      user: null,
      cookiePresent: true,
      accountConsoleUrl: null,
    });

    const thrown = (await requireKcSession({
      pathname: '/a b/c',
      searchStr: '?q=x&y=1',
    }).catch((e: unknown) => e)) as { options: { href: string } };

    const expectedReturn = encodeURIComponent('/a b/c?q=x&y=1');
    expect(thrown.options.href).toBe(`/auth/sign-in?return=${expectedReturn}`);
    // Sanity: the raw, unencoded path must not leak into the href.
    expect(thrown.options.href).not.toContain('/a b/c');
  });

  it('hard-navigates via window.location on the client pass', async () => {
    // Client branch: the gate sets `window.location.href` and then awaits an
    // unresolving promise (the document unload takes over), so we must NOT await
    // the returned promise — it never settles by design. We stub `window`, flush
    // microtasks until the assignment lands, then assert without awaiting.
    const loc = { href: '' };
    vi.stubGlobal('window', { location: loc });
    getServerSessionMock.mockResolvedValue({
      user: null,
      cookiePresent: false,
      accountConsoleUrl: null,
    });

    // Intentionally float the promise; it resolves never (by design).
    void requireKcSession(location);
    // Flush the microtask queue past the awaited getServerSession so the
    // synchronous `window.location.href = target` assignment runs.
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(loc.href).toBe('/auth/sign-in?return=%2Fspaces%2Fabc%3Ftab%3Dassets');
  });
});
