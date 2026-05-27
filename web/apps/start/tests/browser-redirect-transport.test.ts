// @vitest-environment jsdom
/**
 * Behavioral spec for the browser redirect transport's AbortSignal
 * support. The transport opens a popup and waits for the broker's
 * callback to post a message back; the renderer can cancel by passing
 * an AbortSignal that triggers settlement-as-popup-closed (and closes
 * the popup if it's still open).
 *
 * Why this matters: the "Cancel sign-in" button in the login card
 * fires `cancelBrokerFlow()` which aborts the controller wrapping
 * this call. If that signal isn't honored, the user is stuck waiting
 * on a popup they can't easily kill.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserRedirectTransport } from '../src/lib/browser-redirect-transport';

interface FakePopup {
  closed: boolean;
  close: ReturnType<typeof vi.fn>;
}

describe('BrowserRedirectTransport runBrokerOAuth — abort', () => {
  let openSpy: ReturnType<typeof vi.spyOn>;
  let popup: FakePopup;

  beforeEach(() => {
    popup = { closed: false, close: vi.fn(() => void (popup.closed = true)) };
    // Spy on window.open so we can hand back our fake popup. Casting
    // through unknown is the standard escape for the vi.spyOn type
    // mismatch between Window['open'] and our typed fake.
    openSpy = vi
      .spyOn(window, 'open')
      .mockImplementation(() => popup as unknown as Window);
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    openSpy.mockRestore();
    vi.restoreAllMocks();
  });

  it('resolves as popup_closed when the signal aborts mid-flight AND clears all timers + listeners', async () => {
    const transport = new BrowserRedirectTransport();
    const controller = new AbortController();
    const messageListenerSpy = vi.spyOn(window, 'removeEventListener');
    const promise = transport.runBrokerOAuth({
      provider: 'google',
      signal: controller.signal,
    });

    // Give the transport a microtask to register its listeners — the
    // settle path runs synchronously on abort, but we want to assert
    // the result type matches the user-cancelled shape and that the
    // popup was closed.
    await Promise.resolve();
    // After init: a closed-poll setInterval AND a flow-timeout
    // setTimeout should both be live.
    expect(vi.getTimerCount()).toBe(2);

    controller.abort();
    const result = await promise;

    expect(result).toEqual({ ok: false, error: 'popup_closed' });
    expect(popup.close).toHaveBeenCalled();
    // Settle is responsible for tearing down EVERY resource. If a
    // future refactor drops a `clearInterval` / `clearTimeout` from
    // the abort path, this assertion fails before the leak ships.
    expect(vi.getTimerCount()).toBe(0);
    // And the `message` listener is removed via removeEventListener.
    // Other listener removals also fire (storage, abort) — we only
    // pin the one we care about.
    expect(messageListenerSpy).toHaveBeenCalledWith(
      'message',
      expect.any(Function),
    );
  });

  it('resolves immediately as popup_closed without opening a popup when given an already-aborted signal', async () => {
    const transport = new BrowserRedirectTransport();
    const controller = new AbortController();
    controller.abort();

    const result = await transport.runBrokerOAuth({
      provider: 'google',
      signal: controller.signal,
    });

    expect(result).toEqual({ ok: false, error: 'popup_closed' });
    // No popup should have been opened — the shortcut guards against
    // the flash-and-immediately-close behavior that would otherwise
    // occur. Also bumps popup-blocker heuristics for no benefit.
    expect(openSpy).not.toHaveBeenCalled();
  });

  it('resolves as popup_blocked when window.open returns null', async () => {
    openSpy.mockImplementation(() => null);
    const transport = new BrowserRedirectTransport();
    const result = await transport.runBrokerOAuth({ provider: 'google' });
    expect(result).toEqual({ ok: false, error: 'popup_blocked' });
  });

  it('runs without a signal — backward compatible — and cleans up timers when popup is dismissed externally', async () => {
    const transport = new BrowserRedirectTransport();
    // No signal — verify the call still opens the popup. Simulate
    // the user dismissing the popup so the closed-poll catches it
    // and the promise settles. The cleanup assertion below pins
    // that even the no-signal path tears down all timers — without
    // it, vitest's hanging-process detector would catch leaks via
    // process exit but not via test assertion.
    const promise = transport.runBrokerOAuth({ provider: 'google' });
    expect(openSpy).toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(2);

    popup.closed = true;
    // Advance fake time past the 400ms closed-poll interval so the
    // poll's tick fires settle().
    await vi.advanceTimersByTimeAsync(500);
    const result = await promise;

    expect(result).toEqual({ ok: false, error: 'popup_closed' });
    expect(vi.getTimerCount()).toBe(0);
  });
});
