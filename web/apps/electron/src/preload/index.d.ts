import type { ElectronAPI } from '@electron-toolkit/preload';
import type { BrokerRedirectResult } from '@pivox/features/broker';

interface AuthDeepLinkData {
  token?: string;
  state?: string;
  linked?: string;
  error?: string;
}

interface PivoxAPI {
  startBrokerLogin: (input: {
    provider: string;
    loginHint?: string;
  }) => Promise<BrokerRedirectResult>;
  getBrokerBaseUrl: () => Promise<string>;
  startSocialLogin: (provider: string) => Promise<string>;
  startLinkProvider: (provider: string, idToken: string) => Promise<string>;
  onAuthDeepLink: (callback: (data: AuthDeepLinkData) => void) => () => void;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    api: PivoxAPI;
  }
}
