import type { PivoxAPI } from './auth-api';
import type { ElectronAPI } from '@electron-toolkit/preload';

declare global {
  interface Window {
    electron: ElectronAPI;
    api: PivoxAPI;
  }
}
