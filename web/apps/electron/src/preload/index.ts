import { contextBridge, ipcRenderer } from 'electron'
import { electronAPI } from '@electron-toolkit/preload'

// Custom APIs for renderer
const api = {
  startSocialLogin: (provider: string): Promise<string> =>
    ipcRenderer.invoke('auth:start-social-login', provider),
  startLinkProvider: (provider: string, idToken: string): Promise<string> =>
    ipcRenderer.invoke('auth:start-link-provider', provider, idToken),
  onAuthDeepLink: (
    callback: (data: { token?: string; state?: string; error?: string }) => void,
  ): (() => void) => {
    const handler = (_event: Electron.IpcRendererEvent, data: { token?: string; state?: string; error?: string }): void =>
      callback(data)
    ipcRenderer.on('auth:deep-link', handler)
    return () => ipcRenderer.removeListener('auth:deep-link', handler)
  },
}

// Use `contextBridge` APIs to expose Electron APIs to
// renderer only if context isolation is enabled, otherwise
// just add to the DOM global.
if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld('electron', electronAPI)
    contextBridge.exposeInMainWorld('api', api)
  } catch (error) {
    console.error(error)
  }
} else {
  // @ts-ignore (define in dts)
  window.electron = electronAPI
  // @ts-ignore (define in dts)
  window.api = api
}
