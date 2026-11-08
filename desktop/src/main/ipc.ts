import type { BrowserWindow, IpcMainInvokeEvent, MessageBoxOptions, OpenDialogOptions } from 'electron'
import { authorizedSender, externalHTTPS, safeUnsigned } from './security'
import { SidecarClient, SidecarError } from './sidecar'
import { RunStartOwner } from './runOwner'
import type { StartResult, StartSelection } from '../preload/api'
import type { EventPage } from '../renderer/runState'

type InvokeHandler = (event: IpcMainInvokeEvent, params?: unknown) => Promise<unknown>
type IPC = { handle(channel: string, handler: InvokeHandler): void; removeHandler(channel: string): void }
type FolderDialog = { showOpenDialog(window: BrowserWindow, options: OpenDialogOptions): Promise<{ canceled: boolean; filePaths: string[] }> }
type ExternalShell = { openExternal(url: string): Promise<void> }
type MessageDialog = { showMessageBox(window: BrowserWindow, options: MessageBoxOptions): Promise<{ response: number }> }

type BridgeReply = { ok: true; result: unknown } | {
  ok: false; error: { code: string; message: string; request_id?: string; retryable: boolean }
}

async function bridgeReply(action: () => Promise<unknown>): Promise<BridgeReply> {
  try { return { ok: true, result: await action() } }
  catch (error) {
    const known = error instanceof SidecarError
    return { ok: false, error: {
      code: known ? error.code : 'unavailable',
      message: known ? error.message : 'Local operation unavailable',
      ...(known && error.requestID ? { request_id: error.requestID } : {}),
      retryable: known && error.retryable
    } }
  }
}

export const REQUIRED_METHODS = [
  'projects.grant', 'projects.add', 'projects.list', 'workspaces.add', 'workspaces.list',
  'settings.get', 'settings.update', 'tools.detect', 'documents.open',
  'documents.update', 'documents.save', 'graphs.validate', 'runs.preflight',
  'runs.start', 'runs.cancel', 'runs.snapshot', 'runs.events',
  'history.list', 'history.get', 'history.outcomes', 'history.events',
  'history.evidence.list', 'history.evidence', 'history.pin', 'history.prune'
] as const

const remoteMethods = {
  listProjects: 'projects.list', addWorkspace: 'workspaces.add',
  listWorkspaces: 'workspaces.list', getSettings: 'settings.get',
  updateSettings: 'settings.update', detectTool: 'tools.detect',
  openDocument: 'documents.open', updateDocument: 'documents.update',
  saveDocument: 'documents.save', validateGraph: 'graphs.validate',
  preflightRun: 'runs.preflight', cancelRun: 'runs.cancel',
  snapshotRun: 'runs.snapshot', eventsRun: 'runs.events',
  listHistory: 'history.list', getHistory: 'history.get',
  outcomesHistory: 'history.outcomes', eventsHistory: 'history.events',
  listEvidence: 'history.evidence.list', evidenceChunk: 'history.evidence',
  pinHistory: 'history.pin', pruneHistory: 'history.prune'
} as const

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new SidecarError('invalid_request', 'Invalid method parameters')
  }
  return value as Record<string, unknown>
}

function validateNumbers(value: Record<string, unknown>): void {
  for (const key of ['revision', 'expected_revision', 'after_sequence', 'offset', 'limit', 'max_concurrency', 'retention_days']) {
    if (Object.hasOwn(value, key)) safeUnsigned(value[key])
  }
}

function validateHistoryAvailability(value: Record<string, unknown>): void {
  if (value.history_availability !== undefined &&
      ![1, 2, 3].includes(value.history_availability as number)) {
    throw new SidecarError('unavailable', 'Invalid run history availability', true)
  }
}

function validatedEventPage(result: unknown): EventPage {
  const value = object(result)
  for (const key of ['after_sequence', 'next_after', 'watermark']) safeUnsigned(value[key])
  if (!Array.isArray(value.events) || !Array.isArray(value.event_hashes) ||
    !Array.isArray(value.omissions) || value.omissions.length > 16384 ||
    value.events.length !== value.event_hashes.length) {
    throw new SidecarError('unavailable', 'Invalid retained event page')
  }
  for (const event of value.events) safeUnsigned(object(event).sequence)
  for (const raw of value.omissions) {
    const omission = object(raw)
    const from = safeUnsigned(omission.from)
    const to = safeUnsigned(omission.to)
    if (from < 1 || to < from || to > (value.next_after as number) ||
      !['sensitive_identity', 'not_retained'].includes(omission.reason as string)) {
      throw new SidecarError('unavailable', 'Invalid retained event omission')
    }
  }
  const stats = object(value.stats)
  for (const key of ['retained_omitted_events', 'retained_omitted_bytes',
    'live_omitted_events', 'live_omitted_bytes']) safeUnsigned(stats[key])
  return value as EventPage
}

function validatedResultNumbers(method: string, result: unknown): void {
  if (['documents.open', 'documents.update', 'documents.save',
    'settings.get', 'settings.update'].includes(method)) {
    safeUnsigned(object(result).revision)
  }
  if (method === 'runs.preflight') {
    const value = object(result)
    if (value.revision !== undefined) safeUnsigned(value.revision)
  }
}

function stringField(value: Record<string, unknown>, key: string): void {
  if (typeof value[key] !== 'string') throw new SidecarError('unavailable', 'Invalid local service response', true)
}

function booleanField(value: Record<string, unknown>, key: string): void {
  if (typeof value[key] !== 'boolean') throw new SidecarError('unavailable', 'Invalid local service response', true)
}

function optionalString(value: Record<string, unknown>, key: string): void {
  if (value[key] !== undefined) stringField(value, key)
}

function diagnosticSummary(raw: unknown): void {
  const value = object(raw)
  if (!Array.isArray(value.diagnostics)) throw new SidecarError('unavailable', 'Invalid diagnostics', true)
  safeUnsigned(value.diagnostics_total)
  safeUnsigned(value.diagnostics_omitted)
  booleanField(value, 'has_errors')
  for (const entry of value.diagnostics) {
    const diagnostic = object(entry)
    for (const key of ['code', 'severity', 'message']) stringField(diagnostic, key)
    for (const key of ['edge_index', 'graph_input_binding_index', 'line', 'column']) {
      if (diagnostic[key] !== undefined) safeUnsigned(diagnostic[key])
    }
  }
}

function validatedRemoteResult(method: string, result: unknown): void {
  validatedResultNumbers(method, result)
  if (method === 'history.list') {
    const page = object(result)
    if (!Array.isArray(page.runs)) throw new SidecarError('unavailable', 'Invalid history list', true)
    safeUnsigned(page.generation)
    optionalString(page, 'next_cursor')
    for (const raw of page.runs) {
      const run = object(raw)
      for (const field of ['run_id', 'project_id', 'graph_id', 'started_at', 'storage_state']) stringField(run, field)
      if (!['active', 'complete', 'interrupted'].includes(String(run.storage_state))) throw new SidecarError('unavailable', 'Invalid history storage state', true)
      optionalString(run, 'ended_at'); optionalString(run, 'runtime_state')
      booleanField(run, 'pinned'); safeUnsigned(run.revision); stringField(run, 'completeness_digest')
      if (!Object.hasOwn(run, 'completeness')) throw new SidecarError('unavailable', 'Incomplete history list row', true)
    }
  }
  if (method === 'history.get') {
    const value = object(result)
    const run = object(value.run)
    for (const field of ['run_id', 'project_id', 'workspace_id', 'document_id', 'graph_id', 'graph_hash', 'started_at', 'storage_state']) stringField(run, field)
    if (!['active', 'complete', 'interrupted'].includes(String(run.storage_state))) throw new SidecarError('unavailable', 'Invalid history storage state', true)
    optionalString(run, 'ended_at'); optionalString(run, 'runtime_state')
    booleanField(run, 'pinned'); safeUnsigned(run.revision)
    for (const field of ['graph', 'launch', 'completeness']) if (!Object.hasOwn(value, field)) throw new SidecarError('unavailable', 'Incomplete history detail', true)
    for (const field of ['graph_digest', 'launch_digest', 'completeness_digest']) stringField(value, field)
    optionalString(value, 'result_digest')
  }
  if (method === 'history.outcomes') {
    const page = object(result)
    if (!Array.isArray(page.nodes) || !Array.isArray(page.attempts)) throw new SidecarError('unavailable', 'Invalid history outcomes', true)
    optionalString(page, 'next_node_cursor'); optionalString(page, 'next_attempt_cursor')
    for (const raw of page.nodes) {
      const item = object(raw)
      for (const field of ['node_run_id', 'node_id', 'status', 'digest']) stringField(item, field)
      if (!Object.hasOwn(item, 'projection')) throw new SidecarError('unavailable', 'Invalid node outcome', true)
    }
    for (const raw of page.attempts) {
      const item = object(raw)
      for (const field of ['attempt_id', 'node_run_id', 'status', 'digest']) stringField(item, field)
      if (!Object.hasOwn(item, 'projection')) throw new SidecarError('unavailable', 'Invalid attempt outcome', true)
    }
  }
  if (method === 'history.events') {
    const page = object(result)
    if (!Array.isArray(page.events) || !Array.isArray(page.omissions)) throw new SidecarError('unavailable', 'Invalid historical events', true)
    for (const field of ['next_sequence', 'watermark']) safeUnsigned(page[field])
    booleanField(page, 'finished'); booleanField(page, 'complete'); stringField(page, 'completeness_digest')
    for (const raw of page.events) {
      const item = object(raw); safeUnsigned(item.sequence); stringField(item, 'digest')
      if (!Object.hasOwn(item, 'projection')) throw new SidecarError('unavailable', 'Invalid historical event', true)
    }
    for (const raw of page.omissions) {
      const item = object(raw); safeUnsigned(item.from); safeUnsigned(item.to); stringField(item, 'reason')
      if (Number(item.from) < 1 || Number(item.to) < Number(item.from) || Number(item.to) > Number(page.watermark) ||
          !['sensitive_identity', 'not_retained'].includes(String(item.reason))) throw new SidecarError('unavailable', 'Invalid historical omission', true)
    }
  }
  if (method === 'history.evidence.list') {
    const page = object(result)
    if (!Array.isArray(page.descriptors)) throw new SidecarError('unavailable', 'Invalid evidence descriptors', true)
    optionalString(page, 'next_cursor'); safeUnsigned(page.generation); booleanField(page, 'complete')
    for (const raw of page.descriptors) {
      const item = object(raw)
      if (Object.hasOwn(item, 'relative_path')) throw new SidecarError('unavailable', 'Evidence path must remain local to the service', true)
      for (const field of ['evidence_id', 'run_id', 'kind', 'availability', 'created_at']) stringField(item, field)
      if (!['available', 'unavailable'].includes(String(item.availability))) throw new SidecarError('unavailable', 'Invalid evidence availability', true)
      optionalString(item, 'attempt_id'); optionalString(item, 'node_run_id')
      if (!(typeof item.attempt_id === 'string' && item.attempt_id.length > 0) &&
          !(typeof item.node_run_id === 'string' && item.node_run_id.length > 0)) {
        throw new SidecarError('unavailable', 'Unbound evidence descriptor', true)
      }
      optionalString(item, 'source_path'); optionalString(item, 'capture_group')
      optionalString(item, 'omission_reason'); optionalString(item, 'digest')
      if (item.byte_length !== undefined) safeUnsigned(item.byte_length)
      booleanField(item, 'redacted')
    }
  }
  if (method === 'history.evidence') {
    const item = object(result)
    stringField(item, 'availability'); optionalString(item, 'omission_reason'); optionalString(item, 'digest'); optionalString(item, 'bytes_base64')
    safeUnsigned(item.total_length); safeUnsigned(item.next_offset); booleanField(item, 'end')
  }
  if (method === 'history.pin') { const item = object(result); safeUnsigned(item.revision); booleanField(item, 'pinned') }
  if (method === 'history.prune') {
    const item = object(result)
    for (const field of ['removed_runs', 'removed_files', 'skipped_active', 'skipped_interrupted', 'skipped_pinned', 'skipped_pending']) safeUnsigned(item[field])
  }
  if (method === 'projects.add' || method === 'workspaces.add') {
    const item = object(result)
    for (const key of method === 'projects.add'
      ? ['project_id', 'display_name', 'canonical_path', 'availability']
      : ['workspace_id', 'project_id', 'relative_path', 'availability']) stringField(item, key)
  }
  if (method === 'projects.list' || method === 'workspaces.list') {
    const value = object(result)
    const key = method === 'projects.list' ? 'projects' : 'workspaces'
    if (!Array.isArray(value[key])) throw new SidecarError('unavailable', 'Invalid registry page', true)
    optionalString(value, 'next_cursor')
    for (const item of value[key]) validatedRemoteResult(method === 'projects.list' ? 'projects.add' : 'workspaces.add', item)
  }
  if (method === 'settings.get' || method === 'settings.update') {
    const value = object(result)
    safeUnsigned(value.max_concurrency)
    if (value.history_retention_days !== undefined) safeUnsigned(value.history_retention_days)
    stringField(value, 'theme')
    booleanField(value, 'keep_all_history')
    optionalString(value, 'codex_binary_path')
    optionalString(value, 'git_binary_path')
  }
  if (method === 'tools.detect') {
    const value = object(result)
    stringField(value, 'tool_id')
    stringField(value, 'status')
    optionalString(value, 'path')
    optionalString(value, 'diagnostic')
  }
  if (method.startsWith('documents.')) {
    const value = object(result)
    for (const key of ['document_id', 'project_id', 'relative_path', 'text_base64',
      'disk_digest', 'validity']) stringField(value, key)
    optionalString(value, 'semantic_version')
    diagnosticSummary(value)
  }
  if (method === 'graphs.validate' || method === 'runs.preflight') {
    diagnosticSummary(result)
    if (method === 'runs.preflight') {
      const value = object(result)
      for (const key of ['preflight_token', 'graph_version', 'payload_digest']) optionalString(value, key)
    }
  }
  if (method === 'runs.cancel') {
    const value = object(result)
    stringField(value, 'run_id')
    stringField(value, 'state')
  }
  if (method === 'runs.snapshot') {
    const value = object(result)
    stringField(value, 'run_id')
    stringField(value, 'state')
    booleanField(value, 'finished')
    validateHistoryAvailability(value)
  }
}

export function registerBridge(options: {
  ipc: IPC
  window: () => BrowserWindow | null
  client: () => SidecarClient | null
  dialog: FolderDialog & MessageDialog
  shell: ExternalShell
  runs: RunStartOwner
}): () => void {
  const channels: string[] = []
  const register = (name: string, action: (params: unknown, client: SidecarClient, window: BrowserWindow) => Promise<unknown>): void => {
    const channel = `agentplane:${name}`
    channels.push(channel)
    options.ipc.handle(channel, (event, params) => bridgeReply(async () => {
      const window = options.window()
      if (!authorizedSender(event, window)) throw new SidecarError('unauthorized', 'Unauthorized app frame')
      const client = options.client()
      if (!client?.isReady) throw new SidecarError('unavailable', 'Local service unavailable')
      return action(params, client, window!)
    }))
  }

  for (const [name, method] of Object.entries(remoteMethods)) {
    register(name, async (raw, client) => {
      const params = raw === undefined ? {} : Object.fromEntries(
        Object.entries(object(raw)).filter(([, value]) => value !== undefined)
      )
      validateNumbers(params)
      const result = await client.request(method, params, method === 'runs.cancel' ? 120_000 : 30_000)
      validatedRemoteResult(method, result)
      if (method === 'runs.events') return validatedEventPage(result)
      if (method === 'runs.snapshot') {
        const value = object(result)
        safeUnsigned(value.trace_watermark)
        if (value.finished === true && typeof value.run_id === 'string' &&
            ['succeeded', 'failed', 'cancelled'].includes(String(value.state))) {
          options.runs.clearAfterConfirmedTerminal(value.run_id)
        }
      }
      if (method === 'runs.cancel') {
        const value = object(result)
        if (typeof value.run_id === 'string' && ['succeeded', 'failed', 'cancelled'].includes(String(value.state))) {
          options.runs.clearAfterConfirmedTerminal(value.run_id)
        }
      }
      return result
    })
  }

  register('startRun', async (raw, client) => {
    const selection = object(raw) as StartSelection
    validateNumbers(selection)
    return options.runs.start(selection, async (payload): Promise<StartResult> => {
      const result = object(await client.request('runs.start', payload, 120_000)) as StartResult
      safeUnsigned(result.trace_watermark)
      validateHistoryAvailability(result as unknown as Record<string, unknown>)
      return result
    })
  })
  register('reattachRun', async (_raw, client) => options.runs.reattach(async (payload) => {
    const result = object(await client.request('runs.start', payload, 120_000)) as StartResult
    safeUnsigned(result.trace_watermark)
    validateHistoryAvailability(result as unknown as Record<string, unknown>)
    return result
  }))
  channels.push('agentplane:currentRun')
  options.ipc.handle('agentplane:currentRun', (event) => bridgeReply(async () => {
    if (!authorizedSender(event, options.window())) throw new SidecarError('unauthorized', 'Unauthorized app frame')
    return options.runs.current()
  }))
  register('chooseProjectFolder', async (_raw, client, window) => {
    const selected = await options.dialog.showOpenDialog(window, { properties: ['openDirectory'] })
    if (selected.canceled || selected.filePaths.length !== 1) return null
    const grant = object(await client.request('projects.grant', { canonical_path: selected.filePaths[0] }))
    if (typeof grant.grant_token !== 'string' || typeof grant.path_digest !== 'string') {
      throw new SidecarError('unavailable', 'Invalid folder grant')
    }
    const added = await client.request('projects.add', { grant_token: grant.grant_token, path_digest: grant.path_digest })
    validatedRemoteResult('projects.add', added)
    return added
  })
  register('openExternal', async (raw, _client, window) => {
    const url = externalHTTPS(object(raw).url)
    const confirmed = await options.dialog.showMessageBox(window, {
      type: 'question', buttons: ['Cancel', 'Open link'], message: `Open external link to ${new URL(url).hostname}?`
    })
    if (confirmed.response !== 1) return
    await options.shell.openExternal(url)
  })
  return () => { for (const channel of channels) options.ipc.removeHandler(channel) }
}
