import { describe, expect, it } from 'vitest'
import { activeRuns, needsAttention, outcomes } from '../src/dashboard'
import type { PendingApproval, RunSummary } from '../src/types'

const run = (id: string, status: string, started: string, graph = 'g'): RunSummary => ({ run_id: id, graph_id: graph, started_at: started, status, event_count: 5 })
const approval = (runId: string, node = 'approve'): PendingApproval => ({ run_id: runId, node_id: node, node_run_id: `${runId}-${node}`, prompt: '', context: [] })

describe('needsAttention', () => {
  const runs = [
    run('a', 'succeeded', '2026-11-14T10:00:00Z'),
    run('b', 'failed', '2026-11-14T11:00:00Z'),
    run('c', 'running', '2026-11-14T12:00:00Z'),
    run('d', 'interrupted', '2026-11-14T09:00:00Z'),
    run('e', 'cancelled', '2026-11-14T13:00:00Z'),
  ]
  it('lists exactly the pending approvals and the failed or interrupted runs, newest first', () => {
    const items = needsAttention(runs, [approval('c')])
    expect(items.map((i) => (i.kind === 'approval' ? `approval:${i.approval.run_id}` : `run:${i.run.run_id}`))).toEqual(['approval:c', 'run:b', 'run:d'])
  })
  it('puts an approval before a run started at the same time', () => {
    const items = needsAttention([run('x', 'failed', '2026-11-14T10:00:00Z'), run('y', 'running', '2026-11-14T10:00:00Z')], [approval('y')])
    expect(items.map((i) => i.kind)).toEqual(['approval', 'run'])
  })
  it('is empty when nothing needs a person', () => {
    expect(needsAttention([run('a', 'succeeded', '2026-11-14T10:00:00Z')], [])).toEqual([])
  })
  it('keeps an approval whose run is not in the list', () => {
    expect(needsAttention([], [approval('zzz')])).toHaveLength(1)
  })
})

describe('activeRuns', () => {
  it('keeps only running runs', () => {
    expect(activeRuns([run('a', 'running', '2026-11-14T10:00:00Z'), run('b', 'failed', '2026-11-14T10:00:00Z')]).map((r) => r.run_id)).toEqual(['a'])
  })
})

describe('outcomes', () => {
  const now = new Date('2026-11-14T12:00:00Z')
  const day = 24 * 3600 * 1000
  const ago = (ms: number) => new Date(now.getTime() - ms).toISOString()
  it('has one dot per run in the last 7 days, oldest first, coloured by status', () => {
    const runs = [run('new', 'failed', ago(1 * day)), run('old', 'succeeded', ago(6 * day)), run('gone', 'succeeded', ago(8 * day)), run('live', 'running', ago(1000)), run('odd', 'cancelled', ago(2 * day))]
    const { dots } = outcomes(runs, now)
    expect(dots.map((d) => d.run.run_id)).toEqual(['old', 'odd', 'new', 'live'])
    expect(dots.map((d) => d.tone)).toEqual(['p', 'n', 'x', 'b'])
  })
  it('keeps empty slots visible and grows past the minimum', () => {
    expect(outcomes([], now)).toEqual({ dots: [], slots: 14 })
    const many = Array.from({ length: 20 }, (_, i) => run(`r${i}`, 'succeeded', ago(i * 1000)))
    expect(outcomes(many, now).slots).toBe(20)
    expect(outcomes([run('a', 'succeeded', ago(1000))], now)).toMatchObject({ slots: 14 })
  })
  it('ignores runs dated in the future', () => {
    expect(outcomes([run('f', 'succeeded', new Date(now.getTime() + day).toISOString())], now).dots).toEqual([])
  })
})
