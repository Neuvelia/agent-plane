import { contextBridge, ipcRenderer } from 'electron'
import type { AgentPlaneAPI } from './api'
import { documentView, encodeText } from './document'

async function call<T>(name: string, params?: unknown): Promise<T> {
  const reply: unknown = await ipcRenderer.invoke(`agentplane:${name}`, params)
  if (typeof reply !== 'object' || reply === null || Array.isArray(reply)) throw new Error('Invalid desktop reply')
  const value = reply as Record<string, unknown>
  if (value.ok === true && Object.hasOwn(value, 'result')) return value.result as T
  if (value.ok === false && typeof value.error === 'object' && value.error !== null) {
    const detail = value.error as Record<string, unknown>
    const error = new Error(typeof detail.message === 'string' ? detail.message : 'Local operation unavailable')
    Object.assign(error, { code: detail.code, requestID: detail.request_id, retryable: detail.retryable })
    throw error
  }
  throw new Error('Invalid desktop reply')
}

// Only these named methods cross the sandbox boundary. No renderer-controlled
// channel or filesystem/process operation is exposed.
const api: AgentPlaneAPI = {
  chooseProjectFolder: () => call('chooseProjectFolder'),
  listProjects: (limit, cursor) => call('listProjects', { limit, cursor }),
  addWorkspace: (projectID, relativePath) => call('addWorkspace', { project_id: projectID, relative_path: relativePath }),
  listWorkspaces: (projectID, limit, cursor) => call('listWorkspaces', { project_id: projectID, limit, cursor }),
  getSettings: () => call('getSettings'),
  updateSettings: (expectedRevision, changes) => call('updateSettings', { expected_revision: expectedRevision, changes }),
  detectTool: (toolID) => call('detectTool', { tool_id: toolID }),
  openDocument: async (projectID, relativePath) => documentView(await call('openDocument', { project_id: projectID, relative_path: relativePath })),
  updateDocument: async (documentID, expectedRevision, text) => documentView(await call('updateDocument', { document_id: documentID, expected_revision: expectedRevision, text_base64: encodeText(text) })),
  saveDocument: async (documentID, expectedRevision, expectedDiskDigest, acceptReformat) => documentView(await call('saveDocument', { document_id: documentID, expected_revision: expectedRevision, expected_disk_digest: expectedDiskDigest, accept_reformat: acceptReformat })),
  validateGraph: (documentID, revision) => call('validateGraph', { document_id: documentID, revision }),
  preflightRun: (selection) => call('preflightRun', selection),
  startRun: (selection) => call('startRun', selection),
  reattachRun: () => call('reattachRun'),
  cancelRun: (runID) => call('cancelRun', { run_id: runID }),
  snapshotRun: (runID) => call('snapshotRun', { run_id: runID }),
  eventsRun: (runID, afterSequence, limit) => call('eventsRun', { run_id: runID, after_sequence: afterSequence, limit }),
  currentRun: () => call('currentRun'),
  onServiceDisconnected: (listener) => {
    const handler = (): void => listener()
    ipcRenderer.on('agentplane:serviceDisconnected', handler)
    return () => ipcRenderer.removeListener('agentplane:serviceDisconnected', handler)
  },
  openExternal: (url) => call('openExternal', { url })
}

contextBridge.exposeInMainWorld('agentplane', Object.freeze(api))
