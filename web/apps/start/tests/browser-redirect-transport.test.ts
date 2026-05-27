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
  });

  afterEach(() => {
    openSpy.mockRestore();
    vi.restoreAllMocks();
  });

  it('resolves as popup_closed when the signal aborts mid-flight', async () => {
    const transport = new BrowserRedirectTransport();
    const controller = new AbortController();
    const promise = transport.runBrokerOAuth({
      provider: 'google',
      signal: controller.signal,
    });

    // Give the transport a microtask to register its listeners — the
    // settle path runs synchronously on abort, but we want to assert
    // the result type matches the user-cancelled shape and that the
    // popup was closed.
    await Promise.resolve();
    controller.abort();

    const result = await promise;
    expect(result).toEqual({ ok: false, error: 'popup_closed' });
    expect(popup.close).toHaveBeenCalled();
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

  it('runs without a signal — backward compatible', async () => {
    const transport = new BrowserRedirectTransport();
    // No signal — verify the call still opens the popup. Simulate
    // the user dismissing the popup so the closed-poll catches it
    // and the promise settles. Without this cleanup, vitest's
    // hanging-process detector reports a leaked setInterval timer.
    const promise = transport.runBrokerOAuth({ provider: 'google' });
    expect(openSpy).toHaveBeenCalled();
    popup.closed = true;
    const result = await promise;
    expect(result).toEqual({ ok: false, error: 'popup_closed' });
  });
});
