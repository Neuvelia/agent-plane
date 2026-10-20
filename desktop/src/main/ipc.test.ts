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
})
