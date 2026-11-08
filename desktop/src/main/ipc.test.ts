import { describe, expect, it } from 'vitest'
import { registerBridge } from './ipc'
import { RunStartOwner } from './runOwner'
import { SidecarError, type SidecarClient } from './sidecar'

function fixture(response?: (method: string) => unknown) {
  const handlers = new Map<string, (event: never, params?: unknown) => Promise<unknown>>()
  const frame = { url: 'file:///app/index.html' }
  const contents = { mainFrame: frame, getURL: () => frame.url }
  const window = { isDestroyed: () => false, webContents: contents }
  const event = { sender: contents, senderFrame: frame }
  const calls: Array<{ method: string; params: unknown }> = []
  const client = { isReady: true, request: async (method: string, params: unknown) => {
    calls.push({ method, params })
    if (method === 'projects.grant') return { grant_token: 'token', path_digest: 'digest' }
    return response?.(method) ?? { project_id: 'project', display_name: 'Project',
      canonical_path: '/chosen/project', availability: 'ready' }
  } }
  const opened: string[] = []
  const runs = new RunStartOwner()
  registerBridge({
    ipc: { handle: (channel, handler) => { handlers.set(channel, handler as never) }, removeHandler: (channel) => { handlers.delete(channel) } },
    window: () => window as never, client: () => client as unknown as SidecarClient,
    dialog: {
      showOpenDialog: async () => ({ canceled: false, filePaths: ['/chosen/project'] }),
      showMessageBox: async () => ({ response: 1 })
    },
    shell: { openExternal: async (url) => { opened.push(url) } }, runs
  })
  const invoke = async (channel: string, params?: unknown, sender: unknown = event): Promise<unknown> => {
    const handler = handlers.get(`agentplane:${channel}`)
    if (!handler) throw new Error('channel not registered')
    const reply = await handler(sender as never, params) as { ok: boolean; result?: unknown;
      error?: { code: string; message: string } }
    if (!reply.ok) throw Object.assign(new Error(reply.error?.message), { code: reply.error?.code })
    return reply.result
  }
  return { handlers, event, invoke, calls, opened, runs }
}

describe('fixed Electron IPC bridge', () => {
  it('rejects other senders and child frames before contacting Go', async () => {
    const app = fixture()
    await expect(app.invoke('getSettings', undefined, { sender: {}, senderFrame: app.event.senderFrame })).rejects.toMatchObject({ code: 'unauthorized' })
    await expect(app.invoke('getSettings', undefined, { sender: app.event.sender, senderFrame: { url: app.event.senderFrame.url } })).rejects.toMatchObject({ code: 'unauthorized' })
    expect(app.calls).toEqual([])
  })

  it('registers named methods only and confines folder grant to main', async () => {
    const app = fixture()
    expect(app.handlers.has('agentplane:invoke')).toBe(false)
    expect(app.handlers.has('agentplane:projects.grant')).toBe(false)
    await expect(app.invoke('chooseProjectFolder')).resolves.toMatchObject({ project_id: 'project' })
    expect(app.calls).toEqual([
      { method: 'projects.grant', params: { canonical_path: '/chosen/project' } },
      { method: 'projects.add', params: { grant_token: 'token', path_digest: 'digest' } }
    ])
  })

  it('blocks unsafe cursor and non-HTTPS external links', async () => {
    const app = fixture()
    await expect(app.invoke('eventsRun', { run_id: 'id', after_sequence: 2 ** 53, limit: 1 })).rejects.toThrow()
    await expect(app.invoke('openExternal', { url: 'javascript:alert(1)' })).rejects.toThrow()
    expect(app.opened).toEqual([])
    await app.invoke('openExternal', { url: 'https://example.com/help' })
    expect(app.opened).toEqual(['https://example.com/help'])
  })

  it('preserves bounded omission ranges and rejects malformed sidecar ranges', async () => {
    const base = { run_id: 'id', after_sequence: 0, events: [], event_hashes: [],
      next_after: 2, has_more: false, watermark: 2, finished: false,
      stats: { retained_omitted_events: 0, retained_omitted_bytes: 0,
        live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false } }
    const good = fixture((method) => method === 'runs.events'
      ? { ...base, omissions: [{ from: 1, to: 2, reason: 'sensitive_identity' }] } : undefined)
    await expect(good.invoke('eventsRun', { run_id: 'id', after_sequence: 0, limit: 1 }))
      .resolves.toMatchObject({ omissions: [{ from: 1, to: 2, reason: 'sensitive_identity' }] })
    const unsafe = fixture((method) => method === 'runs.events'
      ? { ...base, omissions: [{ from: 1, to: 2 ** 53, reason: 'sensitive_identity' }] } : undefined)
    await expect(unsafe.invoke('eventsRun', { run_id: 'id', after_sequence: 0, limit: 1 }))
      .rejects.toMatchObject({ code: 'unavailable' })
  })

  it('releases the main-owned slot after a terminal snapshot and rejects unsafe document revisions', async () => {
    const app = fixture((method) => method === 'runs.snapshot'
      ? { run_id: 'run', state: 'succeeded', finished: true, trace_watermark: 2 }
      : { revision: 2 ** 53 })
    await app.runs.start({ idempotency_key: '00000000-0000-4000-8000-000000000001',
      preflight_token: 'token', payload_digest: 'digest', document_id: 'doc', revision: 1,
      workspace_id: 'space', inputs: {}, max_concurrency: 1 }, async () => ({
      run_id: 'run', state: 'running', graph_version: 'v', trace_watermark: 1 }))
    await expect(app.invoke('openDocument', { project_id: 'project', relative_path: 'graph.yaml' })).rejects.toMatchObject({ code: 'unavailable' })
    await app.invoke('snapshotRun', { run_id: 'run' })
    expect(app.runs.current()).toEqual({ state: 'none' })
  })

  it('preserves a service error code and request identity in its renderer envelope', async () => {
    const app = fixture(() => { throw new SidecarError('conflict', 'Saved file changed', false, 'request-1', true) })
    const handler = app.handlers.get('agentplane:openDocument')!
    const reply = await handler(app.event as never, { project_id: 'project', relative_path: 'graph.yaml' })
    expect(reply).toEqual({ ok: false, error: { code: 'conflict', message: 'Saved file changed',
      request_id: 'request-1', retryable: true } })
  })

  it('routes named history methods and rejects an evidence path in a sidecar descriptor', async () => {
    const history = fixture((method) => method === 'history.list'
      ? { runs: [{ run_id: 'run', project_id: 'project', graph_id: 'graph', started_at: '2026-11-08T00:00:00Z', storage_state: 'complete', runtime_state: 'succeeded', pinned: false, revision: 1, completeness: [1, true, true, true, true, true], completeness_digest: 'digest' }], generation: 2 }
      : undefined)
    await expect(history.invoke('listHistory', { project_id: 'project', limit: 10 }))
      .resolves.toMatchObject({ runs: [{ run_id: 'run' }] })
    expect(history.calls).toEqual([{ method: 'history.list', params: { project_id: 'project', limit: 10 } }])
    const unsafe = fixture((method) => method === 'history.evidence.list'
      ? { descriptors: [{ evidence_id: 'e', run_id: 'r', attempt_id: 'a', kind: 'diff', availability: 'available', created_at: '2026-11-08T00:00:00Z', redacted: false, relative_path: 'evidence/private' }], generation: 1, complete: true }
      : undefined)
    await expect(unsafe.invoke('listEvidence', { run_id: 'r', limit: 1 })).rejects.toMatchObject({ code: 'unavailable' })
  })

  it('omits absent optional retention fields before validating and sending prune', async () => {
    const app = fixture((method) => method === 'history.prune'
      ? { removed_runs: 0, removed_files: 0, skipped_active: 0,
        skipped_interrupted: 0, skipped_pinned: 0, skipped_pending: 0 }
      : undefined)
    await expect(app.invoke('pruneHistory', { retention_days: undefined, keep_all: undefined }))
      .resolves.toMatchObject({ removed_runs: 0 })
    expect(app.calls).toEqual([{ method: 'history.prune', params: {} }])
  })

  it('accepts node-scoped evidence without inventing an attempt identity', async () => {
    const node = fixture((method) => method === 'history.evidence.list'
      ? { descriptors: [{ evidence_id: 'e', run_id: 'r', node_run_id: 'n',
        kind: 'diff', availability: 'available', created_at: '2026-11-08T00:00:00Z',
        redacted: false, byte_length: 0, digest: 'digest' }], generation: 1, complete: true }
      : undefined)
    await expect(node.invoke('listEvidence', { run_id: 'r', limit: 1 }))
      .resolves.toMatchObject({ descriptors: [{ node_run_id: 'n' }] })
    const unbound = fixture((method) => method === 'history.evidence.list'
      ? { descriptors: [{ evidence_id: 'e', run_id: 'r', kind: 'diff',
        availability: 'unavailable', omission_reason: 'capture_failed',
        created_at: '2026-11-08T00:00:00Z', redacted: false }], generation: 1, complete: false }
      : undefined)
    await expect(unbound.invoke('listEvidence', { run_id: 'r', limit: 1 }))
      .rejects.toMatchObject({ code: 'unavailable' })
  })

  it('routes only the four named graph authoring methods and validates their result shapes', async () => {
    const summary = { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false }
    const document = { document_id: 'doc', project_id: 'project', relative_path: 'graph.yaml',
      text_base64: 'eA==', revision: 1, disk_digest: 'digest', validity: 'valid', ...summary }
    const graph = { schema_version: 0, id: 'graph', units: [], inputs: [], nodes: [], edges: [], graph_input_bindings: [], outputs: [] }
    const app = fixture((method) => method === 'documents.create' || method === 'documents.reload' ? document
      : method === 'documents.inspect' ? { revision: 1, validity: 'valid', graph,
        layout: { version: 1, source: 'fallback', nodes: {} }, diagnostics: summary }
        : method === 'documents.preview_edit' ? { base_revision: 1, text_base64: 'eA==', validity: 'valid',
          diagnostics: summary, reformat_required: false, changed_paths: ['nodes.plan'] } : undefined)
    expect(app.handlers.has('agentplane:invoke')).toBe(false)
    await expect(app.invoke('createDocument', { project_id: 'project', relative_path: 'graph.yaml', graph_id: 'graph' }))
      .resolves.toMatchObject({ document_id: 'doc' })
    await expect(app.invoke('reloadDocument', { document_id: 'doc', expected_revision: 1, discard_draft: true }))
      .resolves.toMatchObject({ document_id: 'doc' })
    await expect(app.invoke('inspectDocument', { document_id: 'doc', revision: 1 }))
      .resolves.toMatchObject({ graph: { id: 'graph' } })
    await expect(app.invoke('previewDocumentEdit', { document_id: 'doc', revision: 1,
      operation: { kind: 'add_node', node_id: 'plan', node_type: 'agent' } }))
      .resolves.toMatchObject({ base_revision: 1, changed_paths: ['nodes.plan'] })
    expect(app.calls.map(({ method }) => method)).toEqual([
      'documents.create', 'documents.reload', 'documents.inspect', 'documents.preview_edit'
    ])
  })

  it('rejects malformed visual operations before contacting the sidecar', async () => {
    const app = fixture()
    for (const operation of [
      { kind: 'add_node', node_id: 'plan', node_type: 'human' },
      { kind: 'set_layout', node_id: 'plan', x: 100001, y: 0 },
      { kind: 'set_layout', node_id: 'plan', x: 1.25, y: 0 },
      { kind: 'delete_node', node_id: 'plan' },
      { kind: 'remove_layout', node_id: 'plan', extra: true }
    ]) {
      await expect(app.invoke('previewDocumentEdit', { document_id: 'doc', revision: 1, operation }))
        .rejects.toMatchObject({ code: 'invalid_request' })
    }
    await expect(app.invoke('createDocument', { project_id: 'project', relative_path: 'graph.yaml', graph_id: 'graph', extra: true }))
      .rejects.toMatchObject({ code: 'invalid_request' })
    await expect(app.invoke('reloadDocument', { document_id: 'doc', expected_revision: 1, discard_draft: 'yes' }))
      .rejects.toMatchObject({ code: 'invalid_request' })
    expect(app.calls).toEqual([])
  })

  it('rejects malformed authoring projections and unsafe preview text from the sidecar', async () => {
    const summary = { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false }
    const badInspection = fixture((method) => method === 'documents.inspect'
      ? { revision: 1, validity: 'valid', graph: { schema_version: 0, id: 'graph', nodes: 'not-array' },
        layout: { version: 1, source: 'fallback', nodes: {} }, diagnostics: summary } : undefined)
    await expect(badInspection.invoke('inspectDocument', { document_id: 'doc', revision: 1 }))
      .rejects.toMatchObject({ code: 'unavailable' })
    const badPreview = fixture((method) => method === 'documents.preview_edit'
      ? { base_revision: 2 ** 53, text_base64: 'eA==', validity: 'valid', diagnostics: summary,
        reformat_required: false, changed_paths: [] } : undefined)
    await expect(badPreview.invoke('previewDocumentEdit', { document_id: 'doc', revision: 1,
      operation: { kind: 'remove_layout', node_id: 'plan' } }))
      .rejects.toMatchObject({ code: 'unavailable' })
  })

  it('refuses undeclared nested graph config and extra document or preview fields', async () => {
    const summary = { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false }
    const node = { id: 'plan', type: 'agent', inputs: [], outputs: [], needs_configuration: false }
    const graph = { schema_version: 0, id: 'graph', units: [], inputs: [], nodes: [node], edges: [], graph_input_bindings: [], outputs: [] }
    const inspect = (graphValue: unknown) => ({ revision: 1, validity: 'valid', graph: graphValue,
      layout: { version: 1, source: 'fallback', nodes: { plan: { x: 0, y: 0 } } }, diagnostics: summary })
    const secret = 'private-token'
    const unsafeGraph = fixture((method) => method === 'documents.inspect'
      ? inspect({ ...graph, nodes: [{ ...node, config: { credential: secret } }] }) : undefined)
    await expect(unsafeGraph.invoke('inspectDocument', { document_id: 'doc', revision: 1 }))
      .rejects.toMatchObject({ code: 'unavailable' })
    const unsafeLayout = fixture((method) => method === 'documents.inspect'
      ? inspect({ ...graph, inputs: [{ name: 'task', type: 'string', required: true, secret }] }) : undefined)
    await expect(unsafeLayout.invoke('inspectDocument', { document_id: 'doc', revision: 1 }))
      .rejects.toMatchObject({ code: 'unavailable' })
    const unsafePreview = fixture((method) => method === 'documents.preview_edit'
      ? { base_revision: 1, text_base64: 'eA==', validity: 'valid', diagnostics: summary,
        reformat_required: false, changed_paths: [], secret } : undefined)
    await expect(unsafePreview.invoke('previewDocumentEdit', { document_id: 'doc', revision: 1,
      operation: { kind: 'remove_layout', node_id: 'plan' } }))
      .rejects.toMatchObject({ code: 'unavailable' })
    const unsafeDocument = fixture((method) => method === 'documents.create'
      ? { document_id: 'doc', project_id: 'project', relative_path: 'graph.yaml',
        text_base64: 'eA==', revision: 1, disk_digest: 'digest', validity: 'valid', ...summary, secret } : undefined)
    await expect(unsafeDocument.invoke('createDocument', { project_id: 'project', relative_path: 'graph.yaml', graph_id: 'graph' }))
      .rejects.toMatchObject({ code: 'unavailable' })
  })
})
