'use strict'
const { app, BrowserWindow, dialog, ipcMain } = require('electron')
const { spawn } = require('node:child_process')
const path = require('node:path')
const { isAllowed } = require('./allowlist.cjs')
const { createGraphHandlers } = require('./graphs.cjs')
const { createRecents } = require('./recents.cjs')
const { createRecordingSettings, serveFlags } = require('./recording.cjs')

// The sidecar is the Go engine. The renderer is only a client of its API.
const repoRoot = path.resolve(__dirname, '..', '..')
const binary = process.env.AGENTPLANE_BIN || path.join(repoRoot, 'bin', 'agentplane')
const storeDir = process.env.AGENTPLANE_STORE_DIR || path.join(app.getPath('userData'), 'runs')

let sidecar = null
let api = null // { base, token } - kept in this process only
let ready = null
const streams = new Map() // subId -> { controller, sender }
// Recording is fixed per sidecar process (ADR-0023): a changed preference
// applies when the app next starts the sidecar, never to a running one.
const recordingSettings = createRecordingSettings(path.join(app.getPath('userData'), 'recording.json'))
let session = { recording: false, missing: [] }

function startSidecar() {
  return new Promise((resolve, reject) => {
    const flags = serveFlags(recordingSettings.load(), process.env)
    session = { recording: flags.recording, missing: flags.missing }
    const child = spawn(binary, ['serve', '--store-dir', storeDir, ...flags.args], { stdio: ['pipe', 'pipe', 'pipe'] })
    sidecar = child
    let buffer = ''
    let settled = false
    const fail = (err) => {
      if (!settled) {
        settled = true
        reject(err)
      }
    }
    child.on('error', (err) => fail(new Error(`cannot start ${binary}: ${err.message}. Run "make build" first.`)))
    child.on('exit', (code) => {
      api = null
      fail(new Error(`agentplane serve exited with code ${code}`))
    })
    child.stderr.on('data', () => {}) // never forwarded: it may contain graph output
    child.stdout.on('data', (chunk) => {
      buffer += chunk.toString('utf8')
      const newline = buffer.indexOf('\n')
      if (newline < 0 || settled) return
      try {
        const info = JSON.parse(buffer.slice(0, newline))
        api = { base: `http://127.0.0.1:${info.port}`, token: info.token }
        settled = true
        resolve()
      } catch (err) {
        fail(err)
      }
    })
  })
}

async function request(method, requestPath, body) {
  await ready
  if (!api) throw new Error('the agentplane sidecar is not running')
  const response = await fetch(api.base + requestPath, {
    method,
    headers: { Authorization: `Bearer ${api.token}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await response.text()
  let json = null
  try {
    json = text ? JSON.parse(text) : null
  } catch {
    json = null
  }
  return { status: response.status, body: json }
}

// Server-sent events are parsed here (fetch, because EventSource cannot send an
// Authorization header). Each event is forwarded as the decoded JSON object.
async function stream(sender, subId, runId) {
  const controller = new AbortController()
  streams.set(subId, { controller, sender })
  const end = (reason) => {
    streams.delete(subId)
    if (!sender.isDestroyed()) sender.send('ap:end', subId, reason)
  }
  try {
    await ready
    const response = await fetch(`${api.base}/runs/${encodeURIComponent(runId)}/events`, {
      headers: { Authorization: `Bearer ${api.token}` },
      signal: controller.signal,
    })
    if (!response.ok || !response.body) return end(`http ${response.status}`)
    const decoder = new TextDecoder()
    let buffer = ''
    for await (const chunk of response.body) {
      buffer += decoder.decode(chunk, { stream: true })
      let split
      while ((split = buffer.indexOf('\n\n')) >= 0) {
        const message = buffer.slice(0, split)
        buffer = buffer.slice(split + 2)
        const name = /^event: (.*)$/m.exec(message)?.[1]
        const data = /^data: (.*)$/m.exec(message)?.[1]
        if (name === 'event' && data && !sender.isDestroyed()) sender.send('ap:event', subId, JSON.parse(data))
        if (name === 'end') return end('end')
        if (name === 'error') return end('server_error')
      }
    }
    end('closed')
  } catch (err) {
    end(controller.signal.aborted ? 'aborted' : String(err.message || err))
  }
}

ipcMain.handle('ap:request', (_e, method, requestPath, body) => {
  // Only the exact documented routes are reachable from the renderer (ADR-0021);
  // /graphs/grants and every other route stay main-only.
  if (!isAllowed(method, requestPath)) {
    return { status: 400, body: { error: { code: 'bad_request', message: 'path not allowed' } } }
  }
  return request(method, requestPath, body)
})

// Graph files are chosen here, in native dialogs, and reach the renderer only as
// opaque grants (ADR-0021). The dialog functions are looked up at call time so
// an end-to-end test can stub them.
const graphs = createGraphHandlers({
  request: (method, requestPath, body) => request(method, requestPath, body),
  showOpen: (options) => dialog.showOpenDialog(BrowserWindow.getFocusedWindow() ?? undefined, options),
  showSave: (options) => dialog.showSaveDialog(BrowserWindow.getFocusedWindow() ?? undefined, options),
  recents: createRecents(path.join(app.getPath('userData'), 'recent-graphs.json')),
})
ipcMain.handle('ap:openGraph', () => graphs.openGraph())
ipcMain.handle('ap:newGraph', () => graphs.newGraph())
ipcMain.handle('ap:recentGraphs', () => graphs.recentGraphs())
ipcMain.handle('ap:openRecentGraph', (_e, id) => graphs.openRecentGraph(id))
ipcMain.handle('ap:touchRecent', (_e, grant) => graphs.touchRecent(grant))
ipcMain.handle('ap:closeGraph', (_e, grant) => graphs.closeGraph(grant))
// The renderer reads and changes only the preference; it learns whether this
// session records and which configured names are unset, never their values.
const recordingView = (settings) => ({ ...settings, active: session.recording, missing: [...session.missing] })
ipcMain.handle('ap:recordingSettings', () => recordingView(recordingSettings.load()))
ipcMain.handle('ap:setRecordingSettings', (_e, input) => {
  const result = recordingSettings.save(input)
  return result.ok ? { ok: true, settings: recordingView(result.settings) } : result
})
ipcMain.on('ap:subscribe', (e, subId, runId) => void stream(e.sender, subId, String(runId)))
ipcMain.on('ap:unsubscribe', (_e, subId) => streams.get(subId)?.controller.abort())
ipcMain.handle('ap:pickGraph', async (e) => {
  const win = BrowserWindow.fromWebContents(e.sender)
  const result = await dialog.showOpenDialog(win, {
    title: 'Choose a graph definition',
    properties: ['openFile'],
    filters: [{ name: 'Graph', extensions: ['yaml', 'yml'] }],
  })
  return result.canceled ? null : result.filePaths[0]
})

function createWindow() {
  const win = new BrowserWindow({
    width: 1440,
    height: 900,
    backgroundColor: '#000000',
    title: 'Tarmind',
    // macOS: hide the native title bar so the window is black edge to edge. The
    // renderer's global bar becomes the drag area (class "mac" in styles.css).
    ...(process.platform === 'darwin' ? { titleBarStyle: 'hidden', trafficLightPosition: { x: 16, y: 18 } } : {}),
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
    },
  })
  win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  win.webContents.on('will-navigate', (event) => event.preventDefault())
  // A reload drops the renderer's subscriptions without telling us; stop their
  // streams so they do not keep following runs for a page that no longer exists.
  win.webContents.on('did-start-navigation', (_e, _url, _inPlace, isMainFrame) => {
    if (!isMainFrame) return
    for (const [id, s] of streams) if (s.sender === win.webContents) s.controller.abort()
  })
  win.loadFile(path.join(__dirname, '..', 'dist', 'index.html'))
}

app.whenReady().then(() => {
  ready = startSidecar()
  ready.catch((err) => {
    dialog.showErrorBox('AgentPlane engine unavailable', String(err.message || err))
    app.quit()
  })
  createWindow()
})
app.on('window-all-closed', () => app.quit())
app.on('before-quit', () => {
  for (const s of streams.values()) s.controller.abort()
  // Closing stdin makes `agentplane serve` cancel active runs and exit.
  sidecar?.stdin?.end()
  setTimeout(() => sidecar?.kill(), 1500).unref()
})
