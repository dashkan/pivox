import { resolveSsoProvider } from '@pivox/features/broker';

import type {
  BrokerRedirectResult,
  RedirectTransport,
} from '@pivox/features/broker';

/**
 * Electron renderer `RedirectTransport`: the broker flow runs in the
 * main process (loopback HTTP server / custom scheme — see
 * `main/broker-auth.ts`) and is reached over the `window.api` IPC
 * bridge exposed by the preload script.
 */
export class IpcRedirectTransport implements RedirectTransport {
  // The broker base URL is fixed for the app's lifetime; resolve it
  // once over IPC and cache the value. A failed lookup leaves the cache
  // unset so the next call retries — caching a rejected promise would
  // wedge SSO for the whole session.
  #brokerBaseUrl: string | null = null;

  runBrokerOAuth(input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult> {
    return window.api.startBrokerLogin(input);
  }

  async resolveSsoProvider(email: string): Promise<string | null> {
    this.#brokerBaseUrl ??= await window.api.getBrokerBaseUrl();
    return resolveSsoProvider(email, this.#brokerBaseUrl);
  }
}

/** Shared instance — caches the broker base URL across calls. */
export const ipcRedirectTransport = new IpcRedirectTransport();
