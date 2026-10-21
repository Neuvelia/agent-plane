// Test-only bridge for the frozen F2.0 visual fixture. Never packaged.
const { contextBridge } = require('electron')
const projectID = '00000000-0000-4000-8000-000000000001'
const runID = '00000000-0000-4000-8000-000000000099'
const projects = [
  { project_id: projectID, display_name: 'Backend API', canonical_path: '/fixture/backend-api', availability: 'ready' },
  { project_id: '00000000-0000-4000-8000-000000000002', display_name: 'Thesis experiments', canonical_path: '/fixture/thesis', availability: 'ready' }
]
const workspace = { workspace_id: 'ws_acme_api', project_id: projectID, relative_path: 'ws_acme_api', availability: 'ready' }
const settings = { revision: 1, theme: 'dark', max_concurrency: 4, history_retention_days: 30, keep_all_history: false }
const omission = { retained_omitted_events: 0, retained_omitted_bytes: 0, live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false }
const event = { run_id: runID, sequence: 1, type: 'run_started', graph_id: 'fix-issue', timestamp: '2026-10-20T12:10:00Z', event_schema_version: 7, payload: { node_count: 2 } }
let listeners = new Set()
let workspacePresent = true
let workspaceHold = false
let releaseWorkspace = null
let failNextProjects = false
let projectHold = false
let releaseProjects = null
const control = {
  holdWorkspaces: () => { workspaceHold = true },
  releaseWorkspaces: () => { workspaceHold = false; releaseWorkspace?.(); releaseWorkspace = null },
  holdProjects: () => { projectHold = true },
  releaseProjects: () => { projectHold = false; releaseProjects?.(); releaseProjects = null },
  disconnect: () => { for (const listener of listeners) listener() },
  markSecondProjectMissing: () => { projects[1].availability = 'missing' },
  failProjectsOnce: () => { failNextProjects = true },
  removeWorkspace: () => { workspacePresent = false }
}
const api = {
  chooseProjectFolder: async () => projects[0],
  listProjects: async () => { if (failNextProjects) { failNextProjects = false; throw Object.assign(new Error('stale cursor'), { code: 'conflict' }) } if (projectHold) await new Promise((resolve) => { releaseProjects = resolve }); return { projects } },
  listWorkspaces: async (id) => { if (workspaceHold) await new Promise((resolve) => { releaseWorkspace = resolve }); return { workspaces: id === projectID && workspacePresent ? [workspace] : [] } },
  getSettings: async () => settings,
  updateSettings: async () => { throw Object.assign(new Error('revision conflict'), { code: 'conflict' }) },
  detectTool: async (id) => ({ tool_id: id, status: 'unverified' }),
  currentRun: async () => ({ state: 'known', run_id: runID }),
  reattachRun: async () => null,
  snapshotRun: async () => ({ run_id: runID, state: 'running', finished: false, trace_watermark: 1 }),
  eventsRun: async (_id, after) => ({ run_id: runID, after_sequence: after, events: after === 0 ? [event] : [], event_hashes: after === 0 ? [`sha256:${'a'.repeat(64)}`] : [], next_after: 1, has_more: false, watermark: 1, stats: omission, finished: false }),
  openDocument: async () => { throw new Error('Visual fixture is read-only') },
  onServiceDisconnected: (listener) => { listeners.add(listener); return () => listeners.delete(listener) }
}
contextBridge.exposeInMainWorld('agentplane', Object.freeze(api))

contextBridge.exposeInMainWorld('visualFixture', Object.freeze(control))
