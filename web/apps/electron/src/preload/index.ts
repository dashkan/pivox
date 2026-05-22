import { contextBridge, ipcRenderer } from 'electron';

import type { BrokerRedirectResult } from '@pivox/features/broker';

contextBridge.exposeInMainWorld('api', {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult> =>
    ipcRenderer.invoke('auth:start-broker-login', input),
  getBrokerBaseUrl: (): Promise<string> =>
    ipcRenderer.invoke('app:get-broker-base-url'),
});
