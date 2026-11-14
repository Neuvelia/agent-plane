import { afterEach, describe, expect, it } from 'vitest'
import { ApiError, listApprovals, listRuns } from '../src/api'

const w = globalThis as unknown as { window?: unknown }
function bridge(response: { status: number; body: unknown } | Error) {
  w.window = { agentplane: { request: async () => { if (response instanceof Error) throw response; return response } } }
}
afterEach(() => { delete w.window })

describe('list endpoints never turn a failure into an empty list', () => {
  it('returns the list on 200', async () => {
    bridge({ status: 200, body: { runs: [{ run_id: 'a' }] } })
    expect(await listRuns()).toEqual([{ run_id: 'a' }])
    bridge({ status: 200, body: { approvals: [] } })
    expect(await listApprovals()).toEqual([])
  })
  it('throws ApiError for a non-200 status, a missing body or a malformed body', async () => {
    for (const r of [{ status: 500, body: null }, { status: 401, body: { error: { message: 'x' } } }, { status: 200, body: null }, { status: 200, body: { runs: 'no' } }]) {
      bridge(r)
      await expect(listRuns()).rejects.toBeInstanceOf(ApiError)
    }
  })
  it('lets a rejected request (sidecar not running) reject', async () => {
    bridge(new Error('the agentplane sidecar is not running'))
    await expect(listApprovals()).rejects.toThrow('sidecar')
  })
})
