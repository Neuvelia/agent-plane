import { describe, expect, it, vi } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import agentEvents from './fixtures/agent-scope-retry.json'
import exhaustedEvents from './fixtures/exhausted.json'
import rejectEvents from './fixtures/demo-reject.json'
import { criticalPath } from '../src/criticalPath'
import { agentTime, MAX_TIMELINE_COLUMNS, timeline } from '../src/insight'
import { filterCounts, filterUnavailable, matchingNodes, slowThresholdMs } from '../src/nodeFilter'
import { outcomeSummary } from '../src/outcome'
import { MAX_EVENT_ROWS, nodeView, reduceAll, settle, type RunView } from '../src/reducer'
import { agentScopeGraph, demoGraph, exhaustedGraph } from '../src/storybook/fixtures'
import type { ApEvent, GraphSummary } from '../src/types'
import { humanWait, queuedWait, waitSummary } from '../src/waits'

// Recorded fixtures (demo-approve, demo-reject, exhausted, agent-scope-retry) plus synthetic
// sequences for what the recordings are too fast to show (F2.6 acceptance criteria).
const approve = approveEvents as unknown as ApEvent[]
const reject = rejectEvents as unknown as ApEvent[]
const exhausted = exhaustedEvents as unknown as ApEvent[]
const agent = agentEvents as unknown as ApEvent[]

const T0 = Date.parse('2026-01-01T00:00:00Z')
let nextSequence = 0
const ev = (type: string, seconds: number, extra: Partial<ApEvent> = {}): ApEvent => ({
  event_schema_version: 11,
  type,
  timestamp: new Date(T0 + seconds * 1000).toISOString(),
  sequence: ++nextSequence,
  run_id: 'r',
  graph_id: 'g',
  graph_version: 'sha256-v0:0123456789abcdef0123',
  payload: {},
  ...extra,
})
const stream = (...parts: (ApEvent | ApEvent[])[]): ApEvent[] => {
  nextSequence = 0
  const out = parts.flat()
  // Renumber in order so every synthetic stream is strictly increasing.
  return out.map((e, i) => ({ ...e, sequence: i + 1 }))
}
const nodeEvents = (node: string, readyAt: number | undefined, startAt: number, endAt: number, status = 'succeeded'): ApEvent[] => [
  ...(readyAt === undefined ? [] : [ev('node_ready', readyAt, { node_id: node })]),
  ev('node_started', startAt, { node_id: node, node_run_id: `${node}-run` }),
  ev('node_finished', endAt, { node_id: node, node_run_id: `${node}-run`, payload: { status, duration_ms: (endAt - startAt) * 1000 } }),
]
const ts = (e: ApEvent) => Date.parse(e.timestamp)
const find = (events: ApEvent[], type: string, node?: string) => events.find((e) => e.type === type && (node === undefined || e.node_id === node))!
const upTo = (events: ApEvent[], type: string, node?: string) => events.slice(0, events.indexOf(find(events, type, node)) + 1)

describe('waiting', () => {
  it('human wait of a synthetic run is the 60 s between request and decision', () => {
    const events = stream(ev('run_started', 0), nodeEvents('h', 0, 0, 61).slice(0, 2), ev('approval_requested', 1, { node_id: 'h', node_run_id: 'h-run' }), ev('approval_decided', 61, { node_id: 'h', node_run_id: 'h-run', payload: { decision: 'approved' } }), ev('node_finished', 61, { node_id: 'h', payload: { status: 'succeeded', duration_ms: 61000 } }), ev('run_finished', 61, { payload: { status: 'succeeded' } }))
    const run = reduceAll(events)
    expect(humanWait(run, nodeView(run, 'h'))).toEqual({ ms: 60000, soFar: false })
    expect(waitSummary(run).humanWaitMs).toBe(60000)
  })

  it('human wait of the recorded demo-approve equals the timestamp difference of its two events', () => {
    const run = reduceAll(approve)
    const reference = ts(find(approve, 'approval_decided')) - ts(find(approve, 'approval_requested'))
    expect(humanWait(run, nodeView(run, 'approve'))).toEqual({ ms: reference, soFar: false })
    expect(waitSummary(run).humanWaitMs).toBe(reference)
  })

  it('queued time is node_started minus node_ready, and unknown (not 0) when node_ready is missing', () => {
    const withReady = reduceAll(stream(ev('run_started', 0), nodeEvents('a', 1, 3, 4), ev('run_finished', 4, { payload: { status: 'succeeded' } })))
    expect(queuedWait(withReady, nodeView(withReady, 'a'))).toEqual({ ms: 2000, soFar: false })
    expect(waitSummary(withReady).queuedMs).toBe(2000)
    const without = reduceAll(stream(ev('run_started', 0), nodeEvents('a', undefined, 3, 4), ev('run_finished', 4, { payload: { status: 'succeeded' } })))
    expect(queuedWait(without, nodeView(without, 'a'))).toBeUndefined()
    expect(waitSummary(without)).toMatchObject({ queuedMs: undefined, unknownNodes: 1 })
  })

  it('a run stopped at approval_requested reports human wait so far, up to the newest event, not the clock', () => {
    const recorded = reduceAll(upTo(approve, 'approval_requested'))
    expect(recorded.status).toBe('running')
    expect(humanWait(recorded, nodeView(recorded, 'approve'))).toEqual({ ms: 0, soFar: true })
    const waiting = reduceAll(stream(ev('run_started', 0), ev('node_ready', 0, { node_id: 'h' }), ev('node_started', 0, { node_id: 'h', node_run_id: 'h-run' }), ev('approval_requested', 1, { node_id: 'h', node_run_id: 'h-run' }), ev('log_chunk', 31, { node_id: 'h', payload: { data: '' } })))
    const before = humanWait(waiting, nodeView(waiting, 'h'))
    expect(before).toEqual({ ms: 30000, soFar: true })
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date(T0 + 3600 * 1000))
      expect(humanWait(waiting, nodeView(waiting, 'h'))).toEqual(before)
    } finally {
      vi.useRealTimers()
    }
    expect(waitSummary(waiting)).toMatchObject({ humanWaitMs: 30000, soFar: true })
  })

  it('a node that is ready and not started yet is queued so far in a running run, and nothing in a finished one', () => {
    const running = reduceAll(stream(ev('run_started', 0), ev('node_ready', 2, { node_id: 'a' }), ev('log_chunk', 7, { node_id: 'b', payload: { data: '' } })))
    expect(queuedWait(running, nodeView(running, 'a'))).toEqual({ ms: 5000, soFar: true })
    const interrupted = settle(running, false)
    expect(queuedWait(interrupted, nodeView(interrupted, 'a'))).toEqual({ ms: 5000, soFar: true })
    const ended = reduceAll(stream(ev('run_started', 0), ev('node_ready', 2, { node_id: 'a' }), ev('node_terminated', 7, { node_id: 'a', payload: { status: 'skipped' } }), ev('run_finished', 7, { payload: { status: 'failed' } })))
    expect(queuedWait(ended, nodeView(ended, 'a'))).toBeUndefined()
  })

  it('a timestamp out of order gives unknown, never a negative wait', () => {
    const run = reduceAll(stream(ev('run_started', 0), ev('node_ready', 5, { node_id: 'a' }), ev('node_started', 3, { node_id: 'a', node_run_id: 'a-run' }), ev('run_finished', 9, { payload: { status: 'succeeded' } })))
    expect(queuedWait(run, nodeView(run, 'a'))).toBeUndefined()
  })

  it('names the longest waits, at most three nodes, longest first, ties by node ID', () => {
    const events = stream(
      ev('run_started', 0),
      [['a', 4], ['b', 1], ['c', 4], ['d', 2]].flatMap(([id, wait]) => nodeEvents(id as string, 0, wait as number, (wait as number) + 1)),
      ev('run_finished', 6, { payload: { status: 'succeeded' } }),
    )
    const longest = waitSummary(reduceAll(events)).longest
    expect(longest.map((w) => [w.nodeId, w.kind, w.ms])).toEqual([['a', 'queue', 4000], ['c', 'queue', 4000], ['d', 'queue', 2000]])
  })
})

describe('agent time', () => {
  it('equals the sum of the agent nodes\' node_finished duration in agent-scope-retry', () => {
    const run = reduceAll(agent)
    const reference = agent.filter((e) => e.type === 'node_finished').reduce((sum, e) => sum + (e.payload.duration_ms as number), 0)
    expect(reference).toBeGreaterThan(0)
    expect(agentTime(run, agentScopeGraph)).toEqual({ kind: 'value', ms: reference })
  })

  it('is not applicable for a graph with no agent node and unknown before an agent node finishes or without a graph', () => {
    expect(agentTime(reduceAll(approve), demoGraph)).toEqual({ kind: 'notApplicable' })
    expect(agentTime(reduceAll(upTo(agent, 'attempt_started')), agentScopeGraph)).toEqual({ kind: 'unknown' })
    expect(agentTime(reduceAll(agent), null)).toEqual({ kind: 'unknown' })
  })
})

describe('critical path', () => {
  const diamond: GraphSummary = {
    nodes: ['a', 'b', 'c', 'd'].map((id) => ({ id, type: 'shell' })),
    edges: [{ from: 'a', to: 'b' }, { from: 'a', to: 'c' }, { from: 'b', to: 'd' }, { from: 'c', to: 'd' }],
  }
  const diamondRun = (seconds: Record<string, number>, skip: string[] = []) => {
    let at = 0
    const parts: ApEvent[][] = []
    for (const id of ['a', 'b', 'c', 'd']) {
      if (skip.includes(id)) {
        parts.push([ev('node_terminated', at, { node_id: id, payload: { status: 'skipped' } })])
        continue
      }
      parts.push(nodeEvents(id, at, at, at + seconds[id]!))
      if (id !== 'b') at += seconds[id]!
    }
    return reduceAll(stream(ev('run_started', 0), parts.flat(), ev('run_finished', at, { payload: { status: 'succeeded' } })))
  }

  it('is the longest chain by duration: a, b, d with total 7 for durations 1, 5, 2, 1', () => {
    const path = criticalPath(diamondRun({ a: 1, b: 5, c: 2, d: 1 }), diamond)!
    expect(path.nodes).toEqual(['a', 'b', 'd'])
    expect(path.totalMs).toBe(7000)
    expect(path.soFar).toBe(false)
  })

  it('breaks a tie by the lower node ID, and every consecutive pair is a graph edge', () => {
    const path = criticalPath(diamondRun({ a: 1, b: 3, c: 3, d: 1 }), diamond)!
    expect(path.nodes).toEqual(['a', 'b', 'd'])
    const edges = new Set(diamond.edges.map((e) => `${e.from}>${e.to}`))
    for (let i = 1; i < path.nodes.length; i++) expect(edges.has(`${path.nodes[i - 1]}>${path.nodes[i]}`)).toBe(true)
  })

  it('leaves skipped nodes off the path and is unknown when no node has a duration', () => {
    const path = criticalPath(diamondRun({ a: 1, b: 5, c: 2, d: 1 }, ['d']), diamond)!
    expect(path.nodes).not.toContain('d')
    expect(path.nodes).toEqual(['a', 'b'])
    expect(criticalPath(reduceAll(stream(ev('run_started', 0))), diamond)).toBeUndefined()
  })

  it('measures an unfinished node to the newest event and says so', () => {
    const run = reduceAll(stream(ev('run_started', 0), nodeEvents('a', 0, 0, 1), ev('node_started', 1, { node_id: 'b', node_run_id: 'b-run' }), ev('log_chunk', 9, { node_id: 'b', payload: { data: '' } })))
    expect(criticalPath(run, diamond)).toEqual({ nodes: ['a', 'b'], totalMs: 9000, soFar: true })
  })

  it('handles a 500-node chain in one linear pass (no recursion limit)', () => {
    const ids = Array.from({ length: 500 }, (_, i) => `n${String(i).padStart(3, '0')}`)
    const chain: GraphSummary = { nodes: ids.map((id) => ({ id, type: 'shell' })), edges: ids.slice(1).map((id, i) => ({ from: ids[i]!, to: id })) }
    const events = stream(ev('run_started', 0), ids.flatMap((id, i) => nodeEvents(id, i, i, i + 1)), ev('run_finished', 500, { payload: { status: 'succeeded' } }))
    const path = criticalPath(reduceAll(events), chain)!
    expect(path.nodes).toEqual(ids)
    expect(path.totalMs).toBe(500000)
  })

  it('on the recorded demo it ends at apply and follows graph edges only', () => {
    const run = reduceAll(approve)
    const path = criticalPath(run, demoGraph)!
    expect(path.nodes[path.nodes.length - 1]).toBe('apply')
    expect(path.nodes).toContain('approve')
    const edges = new Set(demoGraph.edges.map((e) => `${e.from}>${e.to}`))
    for (let i = 1; i < path.nodes.length; i++) expect(edges.has(`${path.nodes[i - 1]}>${path.nodes[i]}`)).toBe(true)
  })
})

describe('what failed', () => {
  it('lists the exhausted loop with its failure kind and the reason of its last red check', () => {
    const run = reduceAll(exhausted)
    const outcome = outcomeSummary(run)!
    const reds = exhausted.filter((e) => e.type === 'gate_check' && e.payload.verdict === 'red')
    expect(outcome.failed).toHaveLength(1)
    expect(outcome.failed[0]).toMatchObject({ nodeId: 'flaky', failureKind: 'needs_intervention', diagnosticCodes: ['E_LOOP_NEEDS_INTERVENTION'], reason: reds[reds.length - 1]!.payload.reason })
    expect(outcome.soFar).toBe(false)
  })

  it('lists the rejected human node with the decision and the skipped dependents', () => {
    const outcome = outcomeSummary(reduceAll(reject))!
    expect(outcome.failed.map((f) => f.nodeId)).toEqual(['approve'])
    expect(outcome.failed[0]!.decision).toEqual({ decision: 'rejected', comment: 'decided by command-line flag' })
    expect(outcome.skipped).toBe(1)
  })

  it('has no card for a succeeded run or a running run with nothing failed', () => {
    expect(outcomeSummary(reduceAll(approve))).toBeUndefined()
    expect(outcomeSummary(reduceAll(approve.slice(0, 12)))).toBeUndefined()
  })

  it('shows failures so far while the run is still going', () => {
    const outcome = outcomeSummary(reduceAll(stream(ev('run_started', 0), nodeEvents('a', 0, 0, 1, 'failed'), ev('node_started', 1, { node_id: 'b', node_run_id: 'b-run' }))))!
    expect(outcome.soFar).toBe(true)
    expect(outcome.failed.map((f) => f.nodeId)).toEqual(['a'])
  })

  it('counts cancelled and interrupted nodes without inventing a failure', () => {
    const cancelled = outcomeSummary(reduceAll(stream(ev('run_started', 0), ev('node_terminated', 1, { node_id: 'a', payload: { status: 'cancelled' } }), ev('run_finished', 1, { payload: { status: 'cancelled' } }))))!
    expect(cancelled).toMatchObject({ failed: [], cancelled: 1 })
    const cut = settle(reduceAll(upTo(approve, 'approval_requested')), false)
    expect(outcomeSummary(cut)).toMatchObject({ failed: [], interrupted: 1 })
  })
})

describe('node filters', () => {
  it('Retried marks exactly flaky in demo-approve, Failed exactly the failed node in exhausted, Human the human node', () => {
    const run = reduceAll(approve)
    expect(matchingNodes(run, demoGraph, 'retried')).toEqual(['flaky'])
    expect(matchingNodes(run, demoGraph, 'human')).toEqual(['approve'])
    expect(matchingNodes(run, demoGraph, 'failed')).toEqual([])
    expect(matchingNodes(reduceAll(exhausted), exhaustedGraph, 'failed')).toEqual(['flaky'])
    expect(matchingNodes(run, demoGraph, 'all')).toEqual(['fast', 'flaky', 'approve', 'apply'])
  })

  it('Slow is max(1 s, 2 x median) of the finished durations and needs three finished nodes', () => {
    const durations = [1, 1, 1, 5]
    const events = stream(ev('run_started', 0), durations.flatMap((d, i) => nodeEvents(`n${i}`, 0, 0, d)), ev('run_finished', 5, { payload: { status: 'succeeded' } }))
    const run = reduceAll(events)
    expect(slowThresholdMs(run)).toBe(2000)
    expect(matchingNodes(run, null, 'slow')).toEqual(['n3'])
    const fast = reduceAll(stream(ev('run_started', 0), [0.1, 0.1, 0.4].flatMap((d, i) => nodeEvents(`n${i}`, 0, 0, d))))
    expect(slowThresholdMs(fast)).toBe(1000)
    expect(matchingNodes(fast, null, 'slow')).toEqual([])
    const two = reduceAll(stream(ev('run_started', 0), [1, 9].flatMap((d, i) => nodeEvents(`n${i}`, 0, 0, d))))
    expect(slowThresholdMs(two)).toBeUndefined()
    expect(filterUnavailable(two, null, 'slow')).toBe('sample')
    expect(matchingNodes(two, null, 'slow')).toEqual([])
  })

  it('Human needs the graph and says so', () => {
    const run = reduceAll(approve)
    expect(filterUnavailable(run, null, 'human')).toBe('graph')
    expect(filterUnavailable(run, demoGraph, 'human')).toBeUndefined()
    expect(matchingNodes(run, null, 'human')).toEqual([])
  })

  it('option counts equal the marked counts', () => {
    const run = reduceAll(approve)
    const counts = filterCounts(run, demoGraph)
    for (const f of ['all', 'failed', 'retried', 'human', 'slow'] as const) expect(counts[f]).toBe(matchingNodes(run, demoGraph, f).length)
    expect(counts.all).toBe(4)
  })
})

describe('timeline segments', () => {
  const run = reduceAll(approve)
  const tl = timeline(run)!
  const start = ts(approve[0]!)

  it('has two attempt segments for flaky whose offsets equal the attempt events\' timestamps', () => {
    const row = tl.rows.find((r) => r.name === 'flaky')!
    const attempts = row.segments.filter((s) => s.kind === 'attempt')
    const started = approve.filter((e) => e.type === 'attempt_started' && e.node_id === 'flaky')
    const finished = approve.filter((e) => e.type === 'attempt_finished' && e.node_id === 'flaky')
    expect(attempts.map((s) => [s.attempt, s.outcome])).toEqual([[1, 'rejected'], [2, 'accepted']])
    expect(attempts.map((s) => s.startSeconds)).toEqual(started.map((e) => (ts(e) - start) / 1000))
    expect(attempts.map((s) => s.endSeconds)).toEqual(finished.map((e) => (ts(e) - start) / 1000))
    expect(row.attempts).toBe(2)
  })

  it('never overlaps two segments of one row and keeps them in time order', () => {
    for (const r of tl.rows) {
      for (let i = 1; i < r.segments.length; i++) expect(r.segments[i]!.startSeconds).toBeGreaterThanOrEqual(r.segments[i - 1]!.endSeconds)
    }
  })

  it('shows the human wait of the human node as its own segment, and queue time as a queued segment', () => {
    const events = stream(ev('run_started', 0), ev('node_ready', 0, { node_id: 'h' }), ev('node_started', 2, { node_id: 'h', node_run_id: 'h-run' }), ev('approval_requested', 3, { node_id: 'h', node_run_id: 'h-run' }), ev('approval_decided', 9, { node_id: 'h', node_run_id: 'h-run', payload: { decision: 'approved' } }), ev('node_finished', 10, { node_id: 'h', payload: { status: 'succeeded', duration_ms: 8000 } }), ev('run_finished', 10, { payload: { status: 'succeeded' } }))
    const row = timeline(reduceAll(events))!.rows[0]!
    expect(row.segments.map((s) => [s.kind, s.startSeconds, s.endSeconds])).toEqual([['queued', 0, 2], ['running', 2, 3], ['human', 3, 9], ['running', 9, 10]])
    expect(row.cells.filter((c) => c === 'human')).toHaveLength(6)
    expect(row.cells[0]).toBe('queued')
  })

  it('gives a retry attempt its own tone and marks an unfinished segment of a running run provisional', () => {
    const row = tl.rows.find((r) => r.name === 'flaky')!
    expect(new Set(row.cells.filter(Boolean)).size).toBeGreaterThan(0)
    const running = timeline(reduceAll(upTo(approve, 'attempt_input')))!
    expect(running.rows.find((r) => r.name === 'flaky')!.segments.some((s) => s.provisional)).toBe(true)
    expect(tl.rows.every((r) => r.segments.every((s) => !s.provisional))).toBe(true)
  })

  it('keeps at most 600 columns, at most 3 x attempts + 2 segments per row and 200 event rows for a 10,000-event run', () => {
    const attempts = 40
    const parts: ApEvent[] = [ev('run_started', 0), ev('node_ready', 0, { node_id: 'loop' }), ev('node_started', 5, { node_id: 'loop', node_run_id: 'loop-run' })]
    for (let a = 1; a <= attempts; a++) {
      parts.push(ev('attempt_started', 100 * a, { node_id: 'loop', attempt_id: `a${a}`, payload: { number: a, max_attempts: attempts } }))
      parts.push(ev('attempt_finished', 100 * a + 60, { node_id: 'loop', attempt_id: `a${a}`, payload: { outcome: a === attempts ? 'accepted' : 'rejected' } }))
    }
    for (let i = 0; parts.length < 9998; i++) parts.push(ev('gate_check', 100 * attempts + 70 + (i % 5), { node_id: 'loop', payload: { verdict: 'red' } }))
    parts.push(ev('node_finished', 100 * attempts + 80, { node_id: 'loop', payload: { status: 'succeeded', duration_ms: 1 } }), ev('run_finished', 100 * attempts + 80, { payload: { status: 'succeeded' } }))
    const big = reduceAll(stream(parts))
    const bigTimeline = timeline(big)!
    expect(big.eventCount).toBeGreaterThanOrEqual(10000)
    expect(bigTimeline.columns).toBeLessThanOrEqual(MAX_TIMELINE_COLUMNS)
    expect(bigTimeline.rows[0]!.cells).toHaveLength(bigTimeline.columns)
    for (const r of bigTimeline.rows) expect(r.segments.length).toBeLessThanOrEqual(3 * Math.max(1, r.attempts) + 2)
    expect(big.tail).toHaveLength(MAX_EVENT_ROWS)
  })
})

describe('live and replay agree', () => {
  const recorded: [string, ApEvent[], GraphSummary][] = [
    ['approve', approve, demoGraph],
    ['reject', reject, demoGraph],
    ['exhausted', exhausted, exhaustedGraph],
    ['agent', agent, agentScopeGraph],
  ]
  const derived = (run: RunView, graph: GraphSummary) => ({
    waits: waitSummary(run),
    path: criticalPath(run, graph),
    outcome: outcomeSummary(run),
    timeline: timeline(run),
    counts: filterCounts(run, graph),
    agent: agentTime(run, graph),
    perNode: run.order.map((id) => [queuedWait(run, run.nodes[id]!), humanWait(run, run.nodes[id]!)]),
  })

  for (const [name, events, graph] of recorded) {
    it(`gives equal derived values for replayed, batched and duplicated delivery (${name})`, () => {
      const replay = derived(reduceAll(events), graph)
      const cut = Math.floor(events.length / 2)
      const batched = reduceAll(events.slice(cut), reduceAll(events.slice(0, cut)))
      const duplicated = reduceAll(events, reduceAll(events.slice(0, cut + 1)))
      expect(derived(batched, graph)).toEqual(replay)
      expect(derived(duplicated, graph)).toEqual(replay)
    })
  }
})
