import { contextBridge, ipcRenderer } from 'electron';

import type { BrokerRedirectResult } from '@pivox/features/broker';

contextBridge.exposeInMainWorld('api', {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
    /**
     * Optional flow identifier. Required only when the caller wants
     * to be able to cancel via `abortBrokerLogin(flowId)`. Other
     * callers (e.g., the profile dialog's "link another provider"
     * action which has no Cancel UI) can omit it and let main
     * generate one internally.
     */
    flowId?: string;
  }): Promise<BrokerRedirectResult> =>
    ipcRenderer.invoke('auth:start-broker-login', input),
  /**
   * Cancel a specific in-flight broker login flow by id. Main
   * settles only the matching flow as `{ ok: false, error:
   * 'popup_closed' }`, tears down its loopback server, and clears
   * its timeout — other flows (if any) are untouched. Resolves once
   * main has processed the request.
   *
   * No-op if the flow has already settled. Used by the renderer when
   * the user clicks "Cancel sign-in" during a social/SSO flow; the
   * renderer remembers the `flowId` it passed to startBrokerLogin
   * and routes the abort through it.
   */
  abortBrokerLogin: (flowId: string): Promise<void> =>
    ipcRenderer.invoke('auth:abort-broker-login', flowId),
});
