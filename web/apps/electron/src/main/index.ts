import { join, resolve } from 'node:path';

import { electronApp, is, optimizer } from '@electron-toolkit/utils';
import { BrowserWindow, app, ipcMain, shell } from 'electron';

import icon from '../../resources/icon.png?asset';

import {
  brokerBaseUrl,
  handleAuthCompleteDeepLink,
  startBrokerLogin,
} from './broker-auth';

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
// The only deep link the app handles is the broker's
// `pivox://auth-complete` callback (the `scheme` transport — see
// broker-auth.ts). Any other URL is ignored.

// macOS: open-url fires when the app is already running or was
// launched via the protocol.
app.on('open-url', (event, url) => {
  event.preventDefault();
  handleAuthCompleteDeepLink(url);
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
    handleAuthCompleteDeepLink(deepLinkUrl);
  }
});

// --- IPC handlers ---

// Broker OAuth flow (loopback / custom-scheme transport — see
// broker-auth.ts). Sign-in vs. account-link is the renderer's
// decision once it holds the returned credential.
ipcMain.handle(
  'auth:start-broker-login',
  async (_event, input: { provider: string; loginHint?: string }) => {
    const result = await startBrokerLogin(input);
    mainWindow?.focus();
    return result;
  },
);

ipcMain.handle('app:get-broker-base-url', () => brokerBaseUrl());

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
    // In dev mode, allow Firebase auth popups (accounts.google.com, etc.)
    if (
      is.dev &&
      (details.url.includes('accounts.google.com') ||
        details.url.includes('github.com/login') ||
        details.url.includes('appleid.apple.com') ||
        details.url.includes('localhost'))
    ) {
      return { action: 'allow' };
    }
    void shell.openExternal(details.url);
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

    // IPC test
    ipcMain.on('ping', () => {
      console.log('pong');
    });

    createWindow();

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
