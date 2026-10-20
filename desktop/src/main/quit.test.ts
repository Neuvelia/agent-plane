import { describe, expect, it } from 'vitest'
import { confirmQuit } from './quit'
import { RunStartOwner } from './runOwner'
import type { SidecarClient } from './sidecar'
import type { StartSelection } from '../preload/api'

const selection: StartSelection = {
  idempotency_key: '00000000-0000-4000-8000-000000000001', preflight_token: 'token',
  payload_digest: 'digest', document_id: 'doc', revision: 1,
  workspace_id: 'space', inputs: {}, max_concurrency: 1
}

async function knownRun(): Promise<RunStartOwner> {
  const owner = new RunStartOwner()
  await owner.start(selection, async () => ({ run_id: 'run', state: 'running',
    graph_version: 'v', trace_watermark: 0 }))
  return owner
}

describe('explicit application Quit', () => {
  it('keeps an active run when the user declines cancellation', async () => {
    const owner = await knownRun()
    let closed = false
    const client = { request: async () => ({ finished: false }),
      close: async () => { closed = true } } as unknown as SidecarClient
    const result = await confirmQuit(client, owner, async () => ({ response: 0 }))
    expect(result).toBe(false)
    expect(closed).toBe(false)
    expect(owner.current().state).toBe('known')
  })

  it('cancels, joins cleanup and releases a confirmed active run', async () => {
    const owner = await knownRun()
    const methods: string[] = []
    const client = { request: async (method: string) => {
      methods.push(method)
      return method === 'runs.snapshot' ? { finished: false } : { state: 'cancelled' }
    }, close: async () => { methods.push('close') } } as unknown as SidecarClient
    expect(await confirmQuit(client, owner, async () => ({ response: 1 }))).toBe(true)
    expect(methods).toEqual(['runs.snapshot', 'runs.cancel', 'close'])
    expect(owner.current().state).toBe('none')
  })

  it('keeps the app open when sidecar cleanup fails after a terminal run', async () => {
    const owner = await knownRun()
    const messages: string[] = []
    const client = { request: async () => ({ finished: true }),
      close: async () => { throw new Error('still live') } } as unknown as SidecarClient
    expect(await confirmQuit(client, owner, async (options) => {
      messages.push(options.message)
      return { response: 0 }
    })).toBe(false)
    expect(messages).toContain('Local service cleanup did not finish. The app remains open.')
  })
})
