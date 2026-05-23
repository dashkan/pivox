import { describe, expect, it, vi } from 'vitest';


import type { BrokerRedirectResult } from '@/shared/redirect-transport';
import type { RedirectTransport } from '@/shared/redirect-transport';

import {
  BROKER_PROVIDER,
  brokerErrorMessage,
  signInViaBroker,
} from '@/shared/broker-auth';

describe('brokerErrorMessage', () => {
  it('treats access_denied and popup_closed as cancellation', () => {
    expect(brokerErrorMessage('access_denied')).toBe('Sign-in was cancelled.');
    expect(brokerErrorMessage('popup_closed')).toBe('Sign-in was cancelled.');
  });

  it('explains a blocked pop-up', () => {
    expect(brokerErrorMessage('popup_blocked')).toMatch(/pop-ups/i);
  });

  it('explains a timeout', () => {
    expect(brokerErrorMessage('auth_timeout')).toMatch(/timed out/i);
  });

  it('falls back to a generic message for unknown codes', () => {
    expect(brokerErrorMessage('something_unexpected')).toBe(
      'Sign-in could not be completed. Please try again.',
    );
  });
});

describe('BROKER_PROVIDER', () => {
  it('maps Firebase provider ids to broker path segments', () => {
    expect(BROKER_PROVIDER['google.com']).toBe('google');
    expect(BROKER_PROVIDER['github.com']).toBe('github');
  });
});

// A RedirectTransport stub whose broker result is fixed per test.
function stubTransport(result: BrokerRedirectResult): RedirectTransport {
  return {
    runBrokerOAuth: () => Promise.resolve(result),
    resolveSsoProvider: () => Promise.resolve(null),
  };
}

describe('signInViaBroker', () => {
  it('surfaces a broker failure through setError and does not sign in', async () => {
    const setError = vi.fn();
    const onSuccess = vi.fn();
    const onLinkRequired = vi.fn();

    await signInViaBroker(
      stubTransport({ ok: false, error: 'auth_timeout' }),
      { provider: 'google' },
      { setError, onSuccess, onLinkRequired },
    );

    expect(setError).toHaveBeenCalledWith('Sign-in timed out. Please try again.');
    expect(onSuccess).not.toHaveBeenCalled();
    expect(onLinkRequired).not.toHaveBeenCalled();
  });

  it('maps a cancelled flow to the cancellation message', async () => {
    const setError = vi.fn();

    await signInViaBroker(
      stubTransport({ ok: false, error: 'access_denied' }),
      { provider: 'github' },
      { setError },
    );

    expect(setError).toHaveBeenCalledWith('Sign-in was cancelled.');
  });

  it('forwards the broker login_hint to the transport', async () => {
    const runBrokerOAuth = vi
      .fn<RedirectTransport['runBrokerOAuth']>()
      .mockResolvedValue({ ok: false, error: 'access_denied' });

    await signInViaBroker(
      { runBrokerOAuth, resolveSsoProvider: () => Promise.resolve(null) },
      { provider: 'oidc.acme', loginHint: 'user@acme.com' },
      { setError: vi.fn() },
    );

    expect(runBrokerOAuth).toHaveBeenCalledWith({
      provider: 'oidc.acme',
      loginHint: 'user@acme.com',
    });
  });
});
