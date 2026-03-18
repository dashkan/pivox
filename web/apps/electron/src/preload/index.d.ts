import type { ElectronAPI } from '@electron-toolkit/preload'

interface AuthDeepLinkData {
  token?: string
  state?: string
  linked?: string
  error?: string
}

interface PivoxAPI {
  startSocialLogin: (provider: string) => Promise<string>
  startLinkProvider: (provider: string, idToken: string) => Promise<string>
  onAuthDeepLink: (callback: (data: AuthDeepLinkData) => void) => () => void
}

declare global {
  interface Window {
    electron: ElectronAPI
    api: PivoxAPI
  }
}
