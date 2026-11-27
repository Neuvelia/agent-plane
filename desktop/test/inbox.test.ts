import { describe, expect, it } from 'vitest'
import approve from './fixtures/demo-approve.json'
import reject from './fixtures/demo-reject.json'
import exhausted from './fixtures/exhausted.json'
import { ageMs, filterQueue, formatAge, inboxQueue, nextItem, queueCounts, recentOutcomes, RECENT_RUNS, selectItem } from '../src/inbox'
import { acceptedNodes, activeNode, changedFiles, failureText, FAILURE_LINES, latestChecks, nodeRows, stopNode } from '../src/inboxRun'
import { reduceAll, settle } from '../src/reducer'
import type { ApEvent, GraphSummary, PendingApproval, RunSummary } from '../src/types'

const run = (id: string, status: string, started: string, graph = 'g'): RunSummary => ({ run_id: id, graph_id: graph, started_at: started, status, event_count: 5 })
const approval = (runId: string, node = 'approve'): PendingApproval => ({ run_id: runId, node_id: node, node_run_id: `${runId}-${node}`, prompt: '', context: [] })

const demoApprove = approve as unknown as ApEvent[]
const demoReject = reject as unknown as ApEvent[]
const exhaustedRetries = exhausted as unknown as ApEvent[]
const demoGraph: GraphSummary = {
  nodes: [{ id: 'fast', type: 'shell' }, { id: 'flaky', type: 'shell', max_attempts: 3 }, { id: 'approve', type: 'human' }, { id: 'apply', type: 'shell' }],
  edges: [{ from: 'fast', to: 'approve' }, { from: 'flaky', to: 'approve' }, { from: 'approve', to: 'apply' }],
}
const upTo = (events: ApEvent[], pred: (e: ApEvent) => boolean) => events.slice(0, events.findIndex(pred) + 1)

describe('inboxQueue', () => {
  const runs = [
    run('a', 'succeeded', '2026-11-14T10:00:00Z'),
    run('b', 'failed', '2026-11-14T11:00:00Z'),
    run('c', 'running', '2026-11-14T12:00:00Z', 'demo'),
    run('d', 'interrupted', '2026-11-14T09:00:00Z'),
    run('e', 'cancelled', '2026-11-14T13:00:00Z'),
    run('f', 'running', '2026-11-14T08:00:00Z'),
  ]
  it('queues approvals, then failed or interrupted runs, then running runs, newest first in each group', () => {
    const items = inboxQueue(runs, [approval('c')])
    expect(items.map((i) => `${i.group}:${i.kind}:${i.key}`)).toEqual(['waiting:approval:c-approve', 'stopped:failed:b', 'stopped:interrupted:d', 'running:running:f'])
  })
  it('shows a running run that waits for a listed approval only as that approval', () => {
    expect(inboxQueue(runs, []).filter((i) => i.runId === 'c').map((i) => i.kind)).toEqual(['running'])
    expect(inboxQueue(runs, [approval('c')]).filter((i) => i.runId === 'c').map((i) => i.kind)).toEqual(['approval'])
  })
  it('names an approval after the graph of its run and dates it by the run start', () => {
    const [first] = inboxQueue(runs, [approval('c')])
    expect(first).toMatchObject({ graphId: 'demo', at: '2026-11-14T12:00:00Z', runId: 'c' })
  })
  it('keeps an approval whose run is not listed, named by its short run ID', () => {
    expect(inboxQueue([], [approval('zzzzzzzzzz')])).toMatchObject([{ kind: 'approval', graphId: 'zzzzzzzz', at: '' }])
  })
  it('leaves out succeeded and cancelled runs', () => {
    expect(inboxQueue([run('a', 'succeeded', '2026-11-14T10:00:00Z'), run('e', 'cancelled', '2026-11-14T10:00:00Z')], [])).toEqual([])
  })
})

describe('filters and selection', () => {
  const items = inboxQueue([run('b', 'failed', '2026-11-14T11:00:00Z'), run('c', 'running', '2026-11-14T12:00:00Z'), run('d', 'running', '2026-11-14T10:00:00Z')], [approval('c')])
  it('counts every group and filters to one', () => {
    expect(queueCounts(items)).toEqual({ all: 3, approvals: 1, stopped: 1, running: 1 })
    expect(filterQueue(items, 'stopped').map((i) => i.key)).toEqual(['b'])
    expect(filterQueue(items, 'running').map((i) => i.key)).toEqual(['d'])
    expect(filterQueue(items, 'all')).toHaveLength(3)
  })
  it('selects the picked item while it is queued, else the first', () => {
    expect(selectItem(items, 'b')?.key).toBe('b')
    expect(selectItem(items, 'gone')?.key).toBe('c-approve')
    expect(selectItem([], null)).toBeUndefined()
  })
  it('moves on to the first other item after a decision', () => {
    expect(nextItem(items, 'c-approve')?.key).toBe('b')
    expect(nextItem(items, 'b')?.key).toBe('c-approve')
  })
})

describe('recentOutcomes', () => {
  it('keeps the newest runs, oldest first, each with the tone of its status', () => {
    const runs = [run('new', 'failed', '2026-11-14T12:00:00Z'), run('old', 'succeeded', '2026-11-14T08:00:00Z'), run('live', 'running', '2026-11-14T13:00:00Z'), run('odd', 'cancelled', '2026-11-14T10:00:00Z')]
    const out = recentOutcomes(runs)
    expect(out.map((o) => o.run.run_id)).toEqual(['old', 'odd', 'new', 'live'])
    expect(out.map((o) => o.tone)).toEqual(['p', 'n', 'x', 'b'])
  })
  it('shows at most RECENT_RUNS runs', () => {
    const many = Array.from({ length: 20 }, (_, i) => run(`r${i}`, 'succeeded', new Date(Date.UTC(2026, 10, 14, 0, i)).toISOString()))
    const out = recentOutcomes(many)
    expect(out).toHaveLength(RECENT_RUNS)
    expect(out[out.length - 1]!.run.run_id).toBe('r19')
  })
})

describe('age', () => {
  const now = new Date('2026-11-14T12:00:00Z')
  it('is unknown for a missing time and never negative', () => {
    expect(ageMs('', now)).toBeUndefined()
    expect(ageMs('2026-11-14T13:00:00Z', now)).toBe(0)
  })
  it('reads in minutes, hours or days, and not at all under a minute', () => {
    expect(formatAge(30_000, 'en')).toBeUndefined()
    expect(formatAge(14 * 60_000, 'en')).toBe('14 min')
    expect(formatAge(3 * 3600_000 + 5, 'pl')).toBe('3 godz.')
    expect(formatAge(2 * 86400_000, 'en')).toMatch(/^2 days?$/)
  })
})

describe('detail selectors over recorded runs', () => {
  it('finds the node a failed loop stopped at and the output of its last check', () => {
    const r = reduceAll(exhaustedRetries)
    const node = stopNode(r)
    expect(node?.id).toBe('flaky')
    expect(node?.attempts).toHaveLength(3)
    const failure = failureText(node!)
    expect(failure.output.split('\n').length).toBeLessThanOrEqual(FAILURE_LINES)
    expect(failure.reason ?? failure.output).not.toBe('')
  })
  it('finds the human node a rejected run stopped at', () => {
    expect(stopNode(reduceAll(demoReject))?.id).toBe('approve')
  })
  it('lists what an interrupted run kept and where it stopped', () => {
    const cut = settle(reduceAll(upTo(demoApprove, (e) => e.type === 'approval_requested')), false)
    expect(cut.status).toBe('interrupted')
    expect(acceptedNodes(cut).sort()).toEqual(['fast', 'flaky'])
    expect(stopNode(cut)?.id).toBe('approve')
  })
  it('names the active node of a running run and what the others wait for', () => {
    const live = reduceAll(upTo(demoApprove, (e) => e.type === 'attempt_started' && e.sequence > 6))
    expect(activeNode(live)).toBeDefined()
    const rows = nodeRows(live, demoGraph)
    expect(rows.map((x) => x.id)).toEqual(['fast', 'flaky', 'approve', 'apply'])
    expect(rows.find((x) => x.id === 'apply')?.waitsFor).toEqual(['approve'])
    expect(rows.find((x) => x.id === 'apply')?.durationMs).toBeUndefined()
  })
  it('keeps the newest check per node and reports no changed files when Git observed none', () => {
    const r = reduceAll(demoApprove)
    expect(latestChecks(r).map((c) => c.nodeId)).toContain('flaky')
    expect(changedFiles(r)).toEqual([])
  })
})
