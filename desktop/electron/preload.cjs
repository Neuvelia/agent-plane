'use strict'
// The renderer gets a narrow, promise-based surface. It never sees the sidecar
// port or token, and has no filesystem, process or network access of its own.
const { contextBridge, ipcRenderer } = require('electron')

let nextSub = 0
const subs = new Map()

ipcRenderer.on('ap:event', (_e, subId, event) => subs.get(subId)?.onEvent(event))
ipcRenderer.on('ap:end', (_e, subId, reason) => {
  subs.get(subId)?.onEnd(reason)
  subs.delete(subId)
})

contextBridge.exposeInMainWorld('agentplane', {
  request: (method, path, body) => ipcRenderer.invoke('ap:request', method, path, body),
  subscribe: (runId, onEvent, onEnd) => {
    const subId = ++nextSub
    subs.set(subId, { onEvent, onEnd })
    ipcRenderer.send('ap:subscribe', subId, runId)
    return () => {
      subs.delete(subId)
      ipcRenderer.send('ap:unsubscribe', subId)
    }
  },
  // Graph files for the editor and New run: native dialogs run in the main
  // process and the renderer receives only an opaque grant and a file name, never a path.
  openGraph: () => ipcRenderer.invoke('ap:openGraph'),
  newGraph: () => ipcRenderer.invoke('ap:newGraph'),
  recentGraphs: () => ipcRenderer.invoke('ap:recentGraphs'),
  openRecentGraph: (id) => ipcRenderer.invoke('ap:openRecentGraph', id),
  touchRecent: (grant) => ipcRenderer.invoke('ap:touchRecent', grant),
  closeGraph: (grant) => ipcRenderer.invoke('ap:closeGraph', grant),
  // "Record run values" (ADR-0023): a preference held by the main process that
  // takes effect when the engine next starts. No environment values pass here.
  recordingSettings: () => ipcRenderer.invoke('ap:recordingSettings'),
  setRecordingSettings: (settings) => ipcRenderer.invoke('ap:setRecordingSettings', settings),
})
