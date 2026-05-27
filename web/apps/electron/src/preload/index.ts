import { contextBridge, ipcRenderer } from 'electron';

import type { BrokerRedirectResult } from '@pivox/features/broker';

contextBridge.exposeInMainWorld('api', {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult> =>
    ipcRenderer.invoke('auth:start-broker-login', input),
  /**
   * Cancel any in-flight broker login flows. Main settles each one
   * as `{ ok: false, error: 'popup_closed' }`, tears down its loopback
   * server, and clears its timeout. Resolves once main has processed
   * the request — used by the renderer when the user clicks "Cancel
   * sign-in" during a social/SSO flow.
   *
   * No-arg because at most one flow is in-flight per renderer in
   * practice. If concurrent flows ever become a real scenario,
   * thread a flow-id from start into abort and key by it.
   */
  abortBrokerLogin: (): Promise<void> =>
    ipcRenderer.invoke('auth:abort-broker-login'),
});
