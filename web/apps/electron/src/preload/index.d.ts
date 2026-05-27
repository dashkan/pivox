import type { ElectronAPI } from '@electron-toolkit/preload';
import type { BrokerRedirectResult } from '@pivox/features/broker';

interface PivoxAPI {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
  }) => Promise<BrokerRedirectResult>;
  abortBrokerLogin: () => Promise<void>;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    api: PivoxAPI;
  }
}
