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

    // Renderer-generated flow id: used both as the CSRF token main
    // round-trips through the broker AND as the IPC key for
    // abortBrokerLogin. Generating here means we can call abort
    // BEFORE startBrokerLogin's promise resolves — the abort IPC
    // targets the specific flow rather than every pending flow.
    const flowId = crypto.randomUUID();

    // Order matters: kick off the IPC FIRST, then attach the abort
    // listener. If startBrokerLogin throws synchronously (e.g., IPC
    // bridge unavailable during teardown), control transfers out of
    // this function before the listener is attached — no leak. The
    // inverse ordering (listener first, then IPC) would orphan the
    // listener on a sync throw.
    const promise = window.api.startBrokerLogin({
      provider: input.provider,
      flowId,
      ...(input.loginHint ? { loginHint: input.loginHint } : {}),
    });

    if (input.signal) {
      const onAbort = (): void => {
        // Best-effort fire-and-forget — if main has already settled
        // the flow (success arrived a tick before the user clicked
        // cancel), abortBrokerLogin is a no-op there.
        void window.api.abortBrokerLogin(flowId);
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
