import { afterEach, describe, expect, it } from 'vitest'
import { ApiError, getRun, listApprovals, listRuns, lookupRun, startRun, validateGraph } from '../src/api'

const w = globalThis as unknown as { window?: unknown }
const sent: { method: string; path: string; body: unknown }[] = []
function bridge(response: { status: number; body: unknown } | Error) {
  sent.length = 0
  w.window = {
    agentplane: {
      request: async (method: string, path: string, body?: unknown) => {
        sent.push({ method, path, body })
        if (response instanceof Error) throw response
        return response
      },
    },
  }
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

describe('New run addresses a graph by grant only', () => {
  it('validates the granted file without sending a path', async () => {
    bridge({ status: 200, body: { status: 'valid', diagnostics: [] } })
    expect(await validateGraph('grant-1')).toEqual({ status: 'valid', diagnostics: [] })
    expect(sent).toEqual([{ method: 'POST', path: '/graphs/validate', body: { grant: 'grant-1' } }])
  })
  it('starts the run with graph_grant and the inputs, never graph_path', async () => {
    bridge({ status: 202, body: { run_id: 'r1' } })
    expect(await startRun('grant-1', ['a=b'])).toEqual({ runId: 'r1' })
    expect(sent).toEqual([{ method: 'POST', path: '/runs', body: { graph_grant: 'grant-1', inputs: ['a=b'] } }])
  })
  it('reports a refused start with the engine message and diagnostics', async () => {
    const diagnostics = [{ severity: 'error', code: 'E_NODE_CONFIG_INVALID', message: 'shell node "work": config.command must not be empty' }]
    bridge({ status: 422, body: { error: { message: 'the graph file is not valid' }, diagnostics } })
    expect(await startRun('grant-1', [])).toEqual({ message: 'the graph file is not valid', diagnostics })
  })
})

describe('looking a run up', () => {
  const found = { run: { run_id: 'a', status: 'succeeded' }, graph: 'g', graph_summary: { nodes: [], edges: [] } }

  it('returns the run on 200', async () => {
    bridge({ status: 200, body: found })
    expect(await lookupRun('a')).toMatchObject({ kind: 'found', graph: 'g' })
    expect(await getRun('a')).toMatchObject({ graph: 'g' })
  })

  it('tells a run from a newer engine apart from a missing one by the typed error code', async () => {
    bridge({ status: 422, body: { error: { code: 'unsupported_schema', message: 'upgrade' } } })
    expect(await lookupRun('a')).toEqual({ kind: 'unsupported' })
    expect(await getRun('a')).toBeNull()
    bridge({ status: 404, body: { error: { code: 'not_found', message: 'unknown run' } } })
    expect(await lookupRun('a')).toEqual({ kind: 'missing' })
    // another 422 is not the unsupported case
    bridge({ status: 422, body: { error: { code: 'something_else', message: 'x' } } })
    expect(await lookupRun('a')).toEqual({ kind: 'missing' })
  })
})
