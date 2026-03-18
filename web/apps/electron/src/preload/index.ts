import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('api', {
  startSocialLogin: (provider: string): Promise<string> =>
    ipcRenderer.invoke('auth:start-social-login', provider),
  startLinkProvider: (provider: string, idToken: string): Promise<string> =>
    ipcRenderer.invoke('auth:start-link-provider', provider, idToken),
  onAuthDeepLink: (
    callback: (data: {
      token?: string;
      state?: string;
      error?: string;
    }) => void,
  ): (() => void) => {
    const handler = (
      _event: Electron.IpcRendererEvent,
      data: { token?: string; state?: string; error?: string },
    ): void => callback(data);
    ipcRenderer.on('auth:deep-link', handler);
    return () => ipcRenderer.removeListener('auth:deep-link', handler);
  },
});
