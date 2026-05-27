import { resolveSsoProvider } from '@pivox/features/broker';

import type {
  BrokerRedirectResult,
  RedirectTransport,
} from '@pivox/features/broker';

// Same env var the main process reads — both sides of Electron see the
// same Pivox app origin without an IPC roundtrip. Falls back to the
// dev ngrok tunnel when unset.
const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';

/**
 * Electron renderer `RedirectTransport`. The broker OAuth flow runs in
 * the main process (loopback HTTP server / custom scheme — see
 * `main/broker-auth.ts`) and is reached over the `window.api` IPC
 * bridge exposed by the preload script. SSO resolution is a same-
 * process HTTP call against the Pivox app origin.
 */
export class ElectronRedirectTransport implements RedirectTransport {
  runBrokerOAuth(input: {
    provider: string;
    loginHint?: string;
    signal?: AbortSignal;
  }): Promise<BrokerRedirectResult> {
    // Already-aborted signal: skip the IPC roundtrip entirely. Main
    // never spawns a flow we'd just have to cancel one tick later.
    if (input.signal?.aborted) {
      return Promise.resolve({ ok: false, error: 'popup_closed' });
    }
    // Forward the OS-browser open to main. The signal listener below
    // hooks an abort to the new abortBrokerLogin IPC; main settles
    // every in-flight flow as popup_closed, which is what
    // startBrokerLogin resolves with.
    const promise = window.api.startBrokerLogin({
      provider: input.provider,
      ...(input.loginHint ? { loginHint: input.loginHint } : {}),
    });

    if (input.signal) {
      const onAbort = (): void => {
        // Best-effort fire-and-forget — if the main process has
        // already settled the flow (success arrived a tick before
        // the user clicked cancel), this is a no-op there.
        void window.api.abortBrokerLogin();
      };
      input.signal.addEventListener('abort', onAbort);
      // Remove the listener after the flow resolves so a later
      // unrelated abort on the same signal (caller reuses the
      // controller for a different operation) doesn't fire a stale
      // IPC. void-cast the floating .finally() — we don't await it.
      void promise.finally(() => {
        input.signal?.removeEventListener('abort', onAbort);
      });
    }

    return promise;
  }

  resolveSsoProvider(email: string): Promise<string | null> {
    return resolveSsoProvider(email, BASE_URL);
  }
}

/** Shared stateless instance — no per-flow state to keep. */
export const electronRedirectTransport = new ElectronRedirectTransport();
