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
  }): Promise<BrokerRedirectResult> {
    return window.api.startBrokerLogin(input);
  }

  resolveSsoProvider(email: string): Promise<string | null> {
    return resolveSsoProvider(email, BASE_URL);
  }
}

/** Shared stateless instance — no per-flow state to keep. */
export const electronRedirectTransport = new ElectronRedirectTransport();
