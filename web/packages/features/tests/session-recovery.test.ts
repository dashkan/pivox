import { describe, expect, it, vi } from 'vitest';

import {
  recoverClientSession,
  type SessionRecoveryDeps,
} from '@/auth/session-recovery';

function deps(overrides: Partial<SessionRecoveryDeps>): SessionRecoveryDeps {
  return {
    getCurrentUserId: () => null,
    mintRecoveryToken: () => Promise.resolve(null),
    signInWithToken: () => Promise.resolve(),
    redirectToLogin: () => {},
    ...overrides,
  };
}

describe('recoverClientSession', () => {
  it('no-ops when a Firebase user is already present', async () => {
    const mint = vi.fn(() => Promise.resolve('tok'));
    const signIn = vi.fn(() => Promise.resolve());
    const redirect = vi.fn();

    const outcome = await recoverClientSession(
      deps({
        getCurrentUserId: () => 'uid-123',
        mintRecoveryToken: mint,
        signInWithToken: signIn,
        redirectToLogin: redirect,
      }),
    );

    expect(outcome).toBe('already-authenticated');
    expect(mint).not.toHaveBeenCalled();
    expect(signIn).not.toHaveBeenCalled();
    expect(redirect).not.toHaveBeenCalled();
  });

  it('mints + signs in silently when the cookie session is recoverable', async () => {
    const signIn = vi.fn(() => Promise.resolve());
    const redirect = vi.fn();

    const outcome = await recoverClientSession(
      deps({
        getCurrentUserId: () => null,
        mintRecoveryToken: () => Promise.resolve('custom-token'),
        signInWithToken: signIn,
        redirectToLogin: redirect,
      }),
    );

    expect(outcome).toBe('recovered');
    expect(signIn).toHaveBeenCalledWith('custom-token');
    expect(redirect).not.toHaveBeenCalled();
  });

  it('redirects to login when there is no recoverable server session', async () => {
    const signIn = vi.fn(() => Promise.resolve());
    const redirect = vi.fn();

    const outcome = await recoverClientSession(
      deps({
        getCurrentUserId: () => null,
        mintRecoveryToken: () => Promise.resolve(null),
        signInWithToken: signIn,
        redirectToLogin: redirect,
      }),
    );

    expect(outcome).toBe('redirected-to-login');
    expect(signIn).not.toHaveBeenCalled();
    expect(redirect).toHaveBeenCalledOnce();
  });

  it('redirects to login when the mint call throws (server unreachable)', async () => {
    const redirect = vi.fn();

    const outcome = await recoverClientSession(
      deps({
        getCurrentUserId: () => null,
        mintRecoveryToken: () => Promise.reject(new Error('server down')),
        redirectToLogin: redirect,
      }),
    );

    expect(outcome).toBe('recovery-failed');
    expect(redirect).toHaveBeenCalledOnce();
  });

  it('redirects to login when signInWithCustomToken fails', async () => {
    const redirect = vi.fn();

    const outcome = await recoverClientSession(
      deps({
        getCurrentUserId: () => null,
        mintRecoveryToken: () => Promise.resolve('bad-token'),
        signInWithToken: () => Promise.reject(new Error('invalid token')),
        redirectToLogin: redirect,
      }),
    );

    expect(outcome).toBe('recovery-failed');
    expect(redirect).toHaveBeenCalledOnce();
  });
});
