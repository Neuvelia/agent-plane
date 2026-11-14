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
  pickGraph: () => ipcRenderer.invoke('ap:pickGraph'),
})
