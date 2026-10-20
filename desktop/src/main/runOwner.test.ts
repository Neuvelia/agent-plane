import { describe, expect, it } from 'vitest'
import { RunStartOwner } from './runOwner'
import { SidecarError } from './sidecar'
import type { StartResult, StartSelection } from '../preload/api'

const selection: StartSelection = {
  idempotency_key: '00000000-0000-4000-8000-000000000001',
  preflight_token: 'token', payload_digest: 'digest',
  document_id: 'document', revision: 1, workspace_id: 'workspace',
  inputs: {}, max_concurrency: 1
}
const accepted: StartResult = { run_id: 'run-id', state: 'running', graph_version: 'v', trace_watermark: 1 }

describe('main-owned Start identity', () => {
  it('retains key and payload before dispatch and joins renderer reload', async () => {
    const owner = new RunStartOwner()
    let release!: (value: StartResult) => void
    const sent: StartSelection[] = []
    const send = (payload: StartSelection): Promise<StartResult> => {
      sent.push(payload)
      return new Promise((resolve) => { release = resolve })
    }
    const first = owner.start(selection, send)
    expect(owner.current()).toEqual({ state: 'pending' })
    const reattached = owner.reattach(send)
    expect(sent).toHaveLength(1)
    release(accepted)
    await expect(first).resolves.toEqual(accepted)
    await expect(reattached).resolves.toEqual(accepted)
    expect(owner.current()).toEqual({ state: 'known', run_id: accepted.run_id })
  })

  it('retries an uncertain response with the same key and logical payload', async () => {
    const owner = new RunStartOwner()
    const sent: StartSelection[] = []
    await expect(owner.start(selection, async (payload) => {
      sent.push(payload)
      throw new Error('response lost')
    })).rejects.toThrow()
    expect(owner.current()).toEqual({ state: 'pending' })
    await expect(owner.reattach(async (payload) => {
      sent.push(payload)
      return accepted
    })).resolves.toEqual(accepted)
    expect(sent).toEqual([selection, selection])
  })

  it('rejects different logical payload under the same key', async () => {
    const owner = new RunStartOwner()
    await owner.start(selection, async () => accepted)
    await expect(owner.start({ ...selection, max_concurrency: 2 }, async () => accepted)).rejects.toThrow()
  })

  it('releases ownership after definitive rejection but retains uncertain transport state', async () => {
    const owner = new RunStartOwner()
    await expect(owner.start(selection, async () => {
      throw new SidecarError('stale_revision', 'Save the graph first')
    })).rejects.toMatchObject({ code: 'stale_revision' })
    expect(owner.current()).toEqual({ state: 'none' })
    await expect(owner.start(selection, async () => {
      throw new SidecarError('unavailable', 'Connection lost', true)
    })).rejects.toMatchObject({ code: 'unavailable', uncertain: true })
    expect(owner.current()).toEqual({ state: 'pending' })
  })

  it('releases ownership for a received unavailable service rejection', async () => {
    const owner = new RunStartOwner()
    await expect(owner.start(selection, async () => {
      throw new SidecarError('unavailable', 'Preparation failed')
    })).rejects.toMatchObject({ code: 'unavailable', uncertain: false })
    expect(owner.current()).toEqual({ state: 'none' })
  })
})
