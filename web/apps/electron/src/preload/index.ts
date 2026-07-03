import { contextBridge, ipcRenderer } from 'electron';

import type { PivoxAPI } from './auth-api';

/**
 * Auth bridge. Every method is a thin pass-through to a main-process IPC handler
 * (keycloak-auth.ts) — the OIDC flow, token lifecycle, and secrets all live in
 * main; the renderer only drives + observes.
 */
const api: PivoxAPI = {
  login: (input) => ipcRenderer.invoke('auth:login', input),
  cancelLogin: () => ipcRenderer.invoke('auth:cancel-login'),
  logout: () => ipcRenderer.invoke('auth:logout'),
  getAuthState: () => ipcRenderer.invoke('auth:get-state'),
  getAccessToken: () => ipcRenderer.invoke('auth:get-access-token'),
  onAuthChanged: (callback) => {
    const listener = (): void => {
      callback();
    };
    ipcRenderer.on('auth:changed', listener);
    return () => {
      ipcRenderer.removeListener('auth:changed', listener);
    };
  },
};

contextBridge.exposeInMainWorld('api', api);
