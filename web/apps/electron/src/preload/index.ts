import { contextBridge, ipcRenderer } from 'electron';

import type { BrokerRedirectResult } from '@pivox/features/broker';

contextBridge.exposeInMainWorld('api', {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult> =>
    ipcRenderer.invoke('auth:start-broker-login', input),
});
