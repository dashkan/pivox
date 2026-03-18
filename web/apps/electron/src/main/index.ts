import { join, resolve } from 'node:path'
import { randomUUID } from 'node:crypto'
import { BrowserWindow, app, ipcMain, shell } from 'electron'
import { electronApp, is, optimizer } from '@electron-toolkit/utils'
import icon from '../../resources/icon.png?asset'

const BASE_URL = process.env.PIVOX_WEB_URL || 'https://pivox.ngrok.app'

let mainWindow: BrowserWindow | null = null
let pendingAuthState: string | null = null

// --- Single instance lock + protocol registration ---

const gotTheLock = app.requestSingleInstanceLock()
if (!gotTheLock) {
  app.quit()
}

if (process.defaultApp) {
  app.setAsDefaultProtocolClient('pivox', process.execPath, [resolve(process.argv[1])])
} else {
  app.setAsDefaultProtocolClient('pivox')
}

// --- Deep link handling ---

function handleDeepLink(url: string): void {
  if (!mainWindow) return

  try {
    const parsed = new URL(url)
    const token = parsed.searchParams.get('token')
    const state = parsed.searchParams.get('state')
    const error = parsed.searchParams.get('error')
    const linked = parsed.searchParams.get('linked')

    if (state && state !== pendingAuthState) {
      mainWindow.webContents.send('auth:deep-link', {
        error: 'State mismatch — possible CSRF attack. Please try again.',
      })
      return
    }

    pendingAuthState = null
    console.log('deep-link received:', { hasToken: !!token, state, linked, error })
    mainWindow.webContents.send('auth:deep-link', { token, state, linked, error })
  } catch {
    mainWindow.webContents.send('auth:deep-link', {
      error: 'Invalid deep link URL.',
    })
  }
}

// macOS: open-url fires when app is already running or launched via protocol
app.on('open-url', (event, url) => {
  event.preventDefault()
  handleDeepLink(url)
})

// Windows/Linux: second-instance fires when a new instance is launched with the protocol URL
app.on('second-instance', (_event, argv) => {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore()
    mainWindow.focus()
  }

  const deepLinkUrl = argv.find((arg) => arg.startsWith('pivox://'))
  if (deepLinkUrl) {
    handleDeepLink(deepLinkUrl)
  }
})

// --- IPC handlers ---

ipcMain.handle('auth:start-social-login', (_event, provider: string) => {
  const state = randomUUID()
  pendingAuthState = state
  const url = `${BASE_URL}/auth/electron-login?provider=${encodeURIComponent(provider)}&state=${encodeURIComponent(state)}`
  shell.openExternal(url)
  return state
})

ipcMain.handle('auth:start-link-provider', (_event, provider: string, idToken: string) => {
  const state = randomUUID()
  pendingAuthState = state
  const url = `${BASE_URL}/auth/electron-link?provider=${encodeURIComponent(provider)}&state=${encodeURIComponent(state)}&token=${encodeURIComponent(idToken)}`
  shell.openExternal(url)
  return state
})

// --- Window creation ---

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 900,
    height: 670,
    show: false,
    autoHideMenuBar: true,
    ...(process.platform === 'linux' ? { icon } : {}),
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      sandbox: false,
    },
  })

  mainWindow.on('ready-to-show', () => {
    mainWindow!.show()
    // Allow Cmd+Option+I to open devtools in production builds for debugging
    mainWindow!.webContents.on('before-input-event', (event, input) => {
      if (input.meta && input.alt && input.key === 'i') {
        mainWindow!.webContents.toggleDevTools()
        event.preventDefault()
      }
    })
  })

  mainWindow.on('closed', () => {
    mainWindow = null
  })

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url)
    return { action: 'deny' }
  })

  // HMR for renderer based on electron-vite cli.
  // Load the remote URL for development or the local html file for production.
  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL'])
  } else {
    mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.whenReady().then(() => {
  // Set app user model id for windows
  electronApp.setAppUserModelId('com.electron')

  // Default open or close DevTools by F12 in development
  // and ignore CommandOrControl + R in production.
  // see https://github.com/alex8088/electron-toolkit/tree/master/packages/utils
  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window)
  })

  // IPC test
  ipcMain.on('ping', () => console.log('pong'))

  createWindow()

  app.on('activate', function () {
    // On macOS it's common to re-create a window in the app when the
    // dock icon is clicked and there are no other windows open.
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})
