import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { app, BrowserWindow, dialog, ipcMain, session, shell } from 'electron'
import { registerBridge, REQUIRED_METHODS } from './ipc'
import { RunStartOwner } from './runOwner'
import { SidecarClient } from './sidecar'
import { confirmQuit } from './quit'

let window: BrowserWindow | null = null
let client: SidecarClient | null = null
let allowQuit = false
let quitting = false
const runs = new RunStartOwner()

function sidecarPath(): string {
  const executable = process.platform === 'win32' ? 'agentplane-desktop.exe' : 'agentplane-desktop'
  const platform = `${process.platform}-${process.arch}`
  return app.isPackaged
    ? join(process.resourcesPath, 'agentplane', 'bin', platform, executable)
    : join(app.getAppPath(), 'build', 'sidecar', platform, executable)
}

function developmentURL(): string | null {
  if (app.isPackaged) return null
  const raw = process.env.ELECTRON_RENDERER_URL
  if (!raw) return null
  const parsed = new URL(raw)
  if (parsed.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(parsed.hostname)) {
    throw new Error('Development renderer must be on loopback')
  }
  return parsed.toString()
}

async function startService(): Promise<void> {
  const binary = sidecarPath()
  if (!existsSync(binary)) throw new Error('Local service binary is missing; build the sidecar first')
  const child = spawn(binary, [], { stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true })
  client = new SidecarClient(child)
  client.onDisconnect(() => {
    runs.markDisconnected()
    if (window && !window.isDestroyed()) window.webContents.send('agentplane:serviceDisconnected')
  })
  try { await client.hello(app.getVersion(), REQUIRED_METHODS) }
  catch { child.stdin.end(); throw new Error('Incompatible local service') }
}

function hardenSession(): void {
  session.defaultSession.setPermissionRequestHandler((_contents, _permission, callback) => callback(false))
  session.defaultSession.setPermissionCheckHandler(() => false)
}

async function createWindow(): Promise<void> {
  window = new BrowserWindow({
    width: 1080, height: 720, minWidth: 900, minHeight: 620, show: false,
    backgroundColor: '#111217',
    webPreferences: {
      preload: join(app.getAppPath(), 'out', 'preload', 'index.cjs'),
      contextIsolation: true, nodeIntegration: false, sandbox: true,
      webviewTag: false
    }
  })
  const contents = window.webContents
  contents.on('will-navigate', (event) => event.preventDefault())
  contents.on('will-attach-webview', (event) => event.preventDefault())
  contents.setWindowOpenHandler(() => ({ action: 'deny' }))
  window.on('close', (event) => {
    if (!allowQuit && !quitting && runs.current().state !== 'none') {
      event.preventDefault()
      app.quit()
    }
  })
  window.on('closed', () => { window = null })
  contents.once('did-finish-load', () => window?.show())
  const dev = developmentURL()
  if (dev) await window.loadURL(dev)
  else await window.loadFile(join(app.getAppPath(), 'out', 'renderer', 'index.html'))
}

app.whenReady().then(async () => {
  hardenSession()
  registerBridge({ ipc: ipcMain, window: () => window, client: () => client,
    dialog, shell, runs })
  try { await startService() }
  catch { /* Show an unavailable state; no sidecar request is treated as success. */ }
  await createWindow()
}).catch(() => { app.quit() })

app.on('before-quit', (event) => {
  if (allowQuit) return
  event.preventDefault()
  if (quitting) return
  quitting = true
  const prompt = (options: Electron.MessageBoxOptions): Promise<{ response: number }> =>
    window && !window.isDestroyed() ? dialog.showMessageBox(window, options) : dialog.showMessageBox(options)
  void confirmQuit(client, runs, prompt).then((confirmed) => {
    quitting = false
    if (confirmed) { allowQuit = true; app.quit() }
  }).catch(() => {
    quitting = false
    // A failed dialog or cleanup is never interpreted as permission to exit.
    dialog.showErrorBox('AgentPlane cannot quit', 'Local run or service cleanup could not be confirmed.')
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

app.on('activate', () => {
  if (window === null || window.isDestroyed()) void createWindow()
})
