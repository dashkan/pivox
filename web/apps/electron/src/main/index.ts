import { join, resolve } from 'node:path';

import { electronApp, is, optimizer } from '@electron-toolkit/utils';
import { BrowserWindow, app, ipcMain, shell } from 'electron';

import icon from '../../resources/icon.png?asset';

import {
  cancelCurrentLogin,
  getAccessToken,
  getAuthState,
  handleAuthCallbackDeepLink,
  login,
  logout,
  onAuthChanged,
  restoreSession,
} from './keycloak-auth';

let mainWindow: BrowserWindow | null = null;

// --- Single instance lock + protocol registration ---

const gotTheLock = app.requestSingleInstanceLock();
if (!gotTheLock) {
  app.quit();
}

if (process.defaultApp) {
  app.setAsDefaultProtocolClient('pivox', process.execPath, [
    resolve(process.argv[1]),
  ]);
} else {
  app.setAsDefaultProtocolClient('pivox');
}

// --- Deep link handling ---
//
// The only deep link the app handles is the OIDC login's
// `pivox://oidc-callback` (the `scheme` transport — see
// oidc-login-flow.ts). Any other URL is ignored.

// macOS: open-url fires when the app is already running or was
// launched via the protocol.
app.on('open-url', (event, url) => {
  event.preventDefault();
  handleAuthCallbackDeepLink(url);
});

// Windows/Linux: second-instance fires when a new instance is launched
// with the protocol URL.
app.on('second-instance', (_event, argv) => {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.focus();
  }

  const deepLinkUrl = argv.find((arg) => arg.startsWith('pivox://'));
  if (deepLinkUrl) {
    handleAuthCallbackDeepLink(deepLinkUrl);
  }
});

// --- IPC handlers ---
//
// The renderer's auth is a thin bridge over these: the OIDC flow, token
// lifecycle, and secrets all live in the main process (see keycloak-auth.ts).

// Start the Authorization Code + PKCE login in the system browser and resolve
// with { ok, error? } once the redirect is caught + exchanged.
ipcMain.handle('auth:login', async (_event, input?: { loginHint?: string }) => {
  const result = await login(input?.loginHint);
  mainWindow?.focus();
  return result;
});

// Cancel an in-flight login (user dismissed the sign-in UI). No-op if none.
ipcMain.handle('auth:cancel-login', () => {
  cancelCurrentLogin();
});

// RP-initiated logout: clears local tokens + ends the Keycloak SSO session.
ipcMain.handle('auth:logout', async () => {
  await logout();
});

// Current auth state ({ ready, user }) — the renderer reads this on mount and
// after every 'auth:changed' event.
ipcMain.handle('auth:get-state', () => getAuthState());

// A valid access token for the renderer's API calls; rejects when signed out
// (the renderer's getAuthToken surfaces that as a 401 → re-auth).
ipcMain.handle('auth:get-access-token', () => getAccessToken());

// --- Window creation ---

function createWindow(): void {
  // Capture the window in a non-null `const` for use throughout this
  // function. `mainWindow` is a mutable module-level `let` that the
  // `closed` handler resets to null, so TS can't narrow it inside the
  // callbacks below — `win` can.
  const win = new BrowserWindow({
    width: 900,
    height: 670,
    show: false,
    autoHideMenuBar: true,
    ...(process.platform === 'linux' ? { icon } : {}),
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      sandbox: true, // AUTHN-03: Enable sandbox for renderer process isolation.
    },
  });
  mainWindow = win;

  win.on('ready-to-show', () => {
    win.show();

    // AUTHN-10: Only allow DevTools in development or when explicitly enabled
    // via PIVOX_ENABLE_DEVTOOLS=1 (for diagnosing production builds).
    if (is.dev || process.env.PIVOX_ENABLE_DEVTOOLS === '1') {
      win.webContents.on('before-input-event', (event, input) => {
        if (input.meta && input.alt && input.key === 'i') {
          win.webContents.toggleDevTools();
          event.preventDefault();
        }
      });
    }
  });

  win.on('closed', () => {
    mainWindow = null;
  });

  win.webContents.setWindowOpenHandler((details) => {
    // Auth happens in the system browser via shell.openExternal (see
    // oidc-login-flow.ts), so the renderer never needs a child window — any
    // window.open target goes to the external browser. Allowlist http(s) only:
    // shell.openExternal would otherwise launch file:// / smb:// / custom
    // schemes handled by other apps.
    try {
      const { protocol } = new URL(details.url);
      if (protocol === 'http:' || protocol === 'https:') {
        void shell.openExternal(details.url);
      }
    } catch {
      // Unparseable URL — deny silently.
    }
    return { action: 'deny' };
  });

  // HMR for renderer based on electron-vite cli.
  // Load the remote URL for development or the local html file for production.
  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    void win.loadURL(process.env['ELECTRON_RENDERER_URL']);
  } else {
    void win.loadFile(join(__dirname, '../renderer/index.html'));
  }
}

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app
  .whenReady()
  .then(() => {
    // Set app user model id for windows
    electronApp.setAppUserModelId('com.electron');

    // Default open or close DevTools by F12 in development
    // and ignore CommandOrControl + R in production.
    // see https://github.com/alex8088/electron-toolkit/tree/master/packages/utils
    app.on('browser-window-created', (_, window) => {
      optimizer.watchWindowShortcuts(window);
    });

    // Push auth-state changes (login / logout / boot restore / refresh-failure
    // sign-out) to the renderer, which re-reads auth:get-state on receipt. A
    // successful silent refresh doesn't fire — the user is unchanged.
    onAuthChanged(() => {
      mainWindow?.webContents.send('auth:changed');
    });

    createWindow();

    // Restore a persisted session in the background — don't block window
    // creation on the network refresh. The renderer shows a splash until the
    // first auth:get-state reports ready (see keycloak-auth.ts).
    void restoreSession();

    app.on('activate', function () {
      // On macOS it's common to re-create a window in the app when the
      // dock icon is clicked and there are no other windows open.
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  })
  .catch((err: unknown) => {
    console.error('[pivox] app startup failed', err);
  });

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});
