import {
  buildBrokerStartUrl,
  parseBrokerRedirect,
  resolveSsoProvider,
} from '@pivox/features/broker';

import type {
  BrokerRedirectResult,
  RedirectTransport,
} from '@pivox/features/broker';

// The broker flow opens in a popup; the popup's final page is
// `/auth/broker-callback`, which postMessages the credential fragment
// back to this window. The web app is served same-origin with the
// broker, so `buildBrokerStartUrl` / `resolveSsoProvider` use the
// current origin.

const POPUP_FEATURES = 'width=600,height=720,menubar=no,toolbar=no';

// Backstop for a popup that never reaches the callback page (broker
// error page, user wanders off). The closed-poll catches an explicit
// close; this catches everything else.
const FLOW_TIMEOUT_MS = 5 * 60 * 1000;

// The message the `/auth/broker-callback` page posts to window.opener.
interface BrokerCallbackMessage {
  type: 'pivox:broker-callback';
  es: string;
  fragment: string;
}

function isBrokerCallbackMessage(data: unknown): data is BrokerCallbackMessage {
  return (
    !!data &&
    typeof data === 'object' &&
    (data as { type?: unknown }).type === 'pivox:broker-callback' &&
    typeof (data as { es?: unknown }).es === 'string' &&
    typeof (data as { fragment?: unknown }).fragment === 'string'
  );
}

/**
 * Browser `RedirectTransport`: drives the broker flow in a popup and
 * recovers the credential fragment via the callback page's
 * `postMessage`. The credential never leaves the URL fragment until
 * that same-origin message hop, so it never reaches a server log.
 */
export class BrowserRedirectTransport implements RedirectTransport {
  runBrokerOAuth(input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult> {
    const origin = window.location.origin;
    // `es` is round-tripped through the broker (preserved on the
    // return URL) and cross-checked when the callback posts back —
    // any other window posting a forged message fails the check.
    const es = crypto.randomUUID();
    const returnUrl = `${origin}/auth/broker-callback?es=${es}`;
    const startUrl = buildBrokerStartUrl({
      baseUrl: origin,
      provider: input.provider,
      returnUrl,
      ...(input.loginHint ? { loginHint: input.loginHint } : {}),
    });

    // A per-flow window name so a second sign-in opens its own popup
    // instead of navigating (and orphaning) the first flow's window.
    const popup = window.open(
      startUrl,
      `pivox-broker-oauth-${es}`,
      POPUP_FEATURES,
    );
    if (!popup) {
      return Promise.resolve({ ok: false, error: 'popup_blocked' });
    }

    return new Promise<BrokerRedirectResult>((resolve) => {
      let settled = false;

      const settle = (result: BrokerRedirectResult): void => {
        if (settled) return;
        settled = true;
        window.removeEventListener('message', onMessage);
        window.clearInterval(closedPoll);
        window.clearTimeout(timer);
        if (!popup.closed) popup.close();
        resolve(result);
      };

      const onMessage = (event: MessageEvent): void => {
        // Restrict to our own callback page: same origin, posted by
        // the popup we opened, carrying the matching `es`.
        if (event.origin !== origin || event.source !== popup) return;
        if (!isBrokerCallbackMessage(event.data)) return;
        if (event.data.es !== es) return;
        settle(parseBrokerRedirect(event.data.fragment));
      };

      const closedPoll = window.setInterval(() => {
        if (popup.closed) settle({ ok: false, error: 'popup_closed' });
      }, 400);

      const timer = window.setTimeout(() => {
        settle({ ok: false, error: 'auth_timeout' });
      }, FLOW_TIMEOUT_MS);

      window.addEventListener('message', onMessage);
    });
  }

  resolveSsoProvider(email: string): Promise<string | null> {
    return resolveSsoProvider(email, window.location.origin);
  }
}

/** Shared stateless instance — the transport holds no per-flow state. */
export const browserRedirectTransport = new BrowserRedirectTransport();
