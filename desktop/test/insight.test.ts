import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import exhaustedEvents from './fixtures/exhausted.json'
import rejectEvents from './fixtures/demo-reject.json'
import { MAX_TIMELINE_COLUMNS, filterEvents, formatDuration, runKpis, timeline } from '../src/insight'
import { MAX_EVENT_ROWS, initialRun, reduce, reduceAll } from '../src/reducer'
import type { ApEvent } from '../src/types'

const approve = approveEvents as unknown as ApEvent[]
const reject = rejectEvents as unknown as ApEvent[]
const exhausted = exhaustedEvents as unknown as ApEvent[]
const ts = (e: ApEvent) => Date.parse(e.timestamp)
const upTo = (events: ApEvent[], type: string) => events.slice(0, events.findIndex((e) => e.type === type) + 1)

// Independent reference: computed straight from the raw events, not from RunView.
function reference(events: ApEvent[]) {
  const started = events.find((e) => e.type === 'run_started')!
  const finished = events.find((e) => e.type === 'run_finished')
  const last = events[events.length - 1]!
  const durations = events.filter((e) => e.type === 'node_finished' || e.type === 'node_terminated').flatMap((e) => (typeof e.payload.duration_ms === 'number' ? [e.payload.duration_ms] : []))
  return {
    wall: ts(finished ?? last) - ts(started),
    nodeTime: durations.length ? durations.reduce((a, b) => a + b, 0) : undefined,
    retries: events.filter((e) => e.type === 'attempt_started' && (e.payload.number as number) > 1).length,
  }
}

describe('runKpis on recorded runs', () => {
  for (const [name, events] of [['approve', approve], ['reject', reject], ['exhausted', exhausted]] as const) {
    it(`matches an independent calculation (${name})`, () => {
      const k = runKpis(reduceAll(events))
      const ref = reference(events)
      expect(k.wallClockMs).toBe(ref.wall)
      expect(k.nodeTimeMs).toBe(ref.nodeTime)
      expect(k.retries).toBe(ref.retries)
      expect(k.savedMs).toBe(ref.nodeTime === undefined ? undefined : Math.max(0, ref.nodeTime - ref.wall))
      expect(k.tokens).toBeUndefined()
    })
  }
  it('counts the demo retry', () => {
    expect(runKpis(reduceAll(approve)).retries).toBe(1)
    expect(runKpis(reduceAll(exhausted)).retries).toBe(2)
  })
  it('reports node time as unknown, not 0, when no node has finished', () => {
    const run = reduceAll(upTo(approve, 'node_started'))
    expect(Object.values(run.nodes).some((n) => n.durationMs !== undefined)).toBe(false)
    expect(runKpis(run).nodeTimeMs).toBeUndefined()
    expect(runKpis(run).savedMs).toBeUndefined()
  })
  it('stops wall clock at the newest event while running and never goes below zero for saved time', () => {
    const events = upTo(approve, 'approval_requested')
    const run = reduceAll(events)
    expect(run.status).toBe('running')
    expect(runKpis(run).wallClockMs).toBe(ts(events[events.length - 1]!) - ts(events[0]!))
    expect(runKpis(run).savedMs).toBeGreaterThanOrEqual(0)
  })
  it('has no wall clock for a run that has not started', () => {
    expect(runKpis(initialRun()).wallClockMs).toBeUndefined()
  })
})

describe('formatDuration', () => {
  it('formats ms, seconds and minutes with the same units in both languages', () => {
    expect(formatDuration(820, 'en')).toBe('820 ms')
    expect(formatDuration(48300, 'en')).toBe('48.3s')
    expect(formatDuration(48300, 'pl')).toBe('48,3s')
    expect(formatDuration(72000, 'en')).toBe('1m 12s')
    expect(formatDuration(120000, 'pl')).toBe('2m')
  })
})

describe('timeline', () => {
  it('has a row for each node that started and marks only the seconds it was active', () => {
    const run = reduceAll(approve)
    const tl = timeline(run)!
    expect(tl.rows.map((r) => r.name).sort()).toEqual(['apply', 'approve', 'fast', 'flaky'])
    expect(tl.bucketSeconds).toBe(1)
    expect(tl.columns).toBe(Math.max(1, Math.ceil(runKpis(run).wallClockMs! / 1000)))
    for (const r of tl.rows) {
      expect(r.active).toHaveLength(tl.columns)
      expect(r.active.some(Boolean)).toBe(true)
    }
  })
  it('marks only the seconds a node was active (synthetic, several seconds)', () => {
    const base = Date.parse('2026-01-01T00:00:00Z')
    const mk = (sequence: number, type: string, seconds: number, extra: Partial<ApEvent> = {}): ApEvent => ({ event_schema_version: 9, type, timestamp: new Date(base + seconds * 1000).toISOString(), sequence, run_id: 'r', graph_id: 'g', graph_version: 'v', payload: {}, ...extra })
    const events = [
      mk(1, 'run_started', 0),
      mk(2, 'node_started', 0, { node_id: 'a', node_run_id: 'a1' }),
      mk(3, 'node_finished', 3, { node_id: 'a', node_run_id: 'a1', payload: { status: 'succeeded', duration_ms: 3000 } }),
      mk(4, 'node_started', 5, { node_id: 'b', node_run_id: 'b1' }),
      mk(5, 'node_finished', 8, { node_id: 'b', node_run_id: 'b1', payload: { status: 'succeeded', duration_ms: 3000 } }),
      mk(6, 'run_finished', 10, { payload: { status: 'succeeded' } }),
    ]
    const tl = timeline(reduceAll(events))!
    expect(tl.columns).toBe(10)
    const dots = (name: string) => tl.rows.find((r) => r.name === name)!.active.map((on) => (on ? 1 : 0)).join('')
    expect(dots('a')).toBe('1111000000')
    expect(dots('b')).toBe('0000011110')
  })
  it('has no rows before any node starts', () => {
    expect(timeline(reduceAll(approve.slice(0, 1)))!.rows).toEqual([])
  })
  it('is undefined when the run has no start time', () => {
    expect(timeline(initialRun())).toBeUndefined()
  })
  it('never exceeds the column cap, however long the run, and reports the bucket size', () => {
    const base = Date.parse('2026-01-01T00:00:00Z')
    const at = (s: number) => new Date(base + s * 1000).toISOString()
    const mk = (sequence: number, type: string, seconds: number, extra: Partial<ApEvent> = {}): ApEvent => ({ event_schema_version: 9, type, timestamp: at(seconds), sequence, run_id: 'r', graph_id: 'g', graph_version: 'v', payload: {}, ...extra })
    let run = reduce(initialRun(), mk(1, 'run_started', 0))
    run = reduce(run, mk(2, 'node_started', 10, { node_id: 'a', node_run_id: 'a1' }))
    run = reduce(run, mk(3, 'node_finished', 9000, { node_id: 'a', node_run_id: 'a1', payload: { status: 'succeeded', duration_ms: 8990000 } }))
    run = reduce(run, mk(4, 'run_finished', 10000, { payload: { status: 'succeeded' } }))
    const tl = timeline(run)!
    expect(tl.columns).toBeLessThanOrEqual(MAX_TIMELINE_COLUMNS)
    expect(tl.bucketSeconds).toBeGreaterThan(1)
    expect(tl.rows[0]!.active).toHaveLength(tl.columns)
    expect(tl.rows[0]!.active.some(Boolean)).toBe(true)
  })
})

describe('event tail', () => {
  it('keeps at most MAX_EVENT_ROWS rows, the newest, for a 10,000-event run', () => {
    let run = initialRun()
    for (let i = 1; i <= 10000; i++) {
      run = reduce(run, { event_schema_version: 9, type: 'gate_check', timestamp: '2026-01-01T00:00:00Z', sequence: i, run_id: 'r', graph_id: 'g', graph_version: 'v', node_id: 'n', payload: { verdict: 'red' } })
    }
    expect(run.eventCount).toBe(10000)
    expect(run.tail).toHaveLength(MAX_EVENT_ROWS)
    expect(run.tail[0]!.sequence).toBe(10000 - MAX_EVENT_ROWS + 1)
    expect(run.tail[run.tail.length - 1]!.sequence).toBe(10000)
  })
  it('leaves log_chunk out (node output has its own view) but counts it', () => {
    const run = reduceAll(approve)
    expect(run.eventCount).toBe(approve.length)
    expect(run.tail.some((e) => e.type === 'log_chunk')).toBe(false)
    expect(run.tail.length).toBe(approve.filter((e) => e.type !== 'log_chunk').length)
  })
  it('keeps unknown event types by name with no filter kind', () => {
    const run = reduce(initialRun(), { event_schema_version: 9, type: 'from_the_future', timestamp: '2026-01-01T00:00:00Z', sequence: 1, run_id: 'r', graph_id: 'g', graph_version: 'v', payload: { x: 1 } })
    expect(run.tail).toEqual([{ sequence: 1, timestamp: '2026-01-01T00:00:00Z', type: 'from_the_future', node: undefined, detail: undefined, kind: undefined }])
  })
  it('tolerates the schema 11 workspace isolation events without interpreting them', () => {
    const base = { event_schema_version: 11, timestamp: '2026-01-01T00:00:00Z', run_id: 'r', graph_id: 'g', graph_version: 'v' }
    const sha = '0123456789abcdef0123456789abcdef01234567'
    const stream: ApEvent[] = [
      { ...base, type: 'workspace_prepared', sequence: 1, node_id: 'n', node_run_id: 'nr', payload: { base_sha: sha, mode: 'worktree', path: '/w', source_root: '/s' } },
      { ...base, type: 'workspace_integrated', sequence: 2, node_id: 'n', node_run_id: 'nr', attempt_id: 'a', payload: { unit_ref: 'refs/agentplane/r/units/n-0123456789abcdef', unit_commit: sha, integrated_ref: 'refs/agentplane/r/integrated', integrated_commit: sha, changed_paths: 1 } },
      { ...base, type: 'workspace_conflict', sequence: 3, node_id: 'n', node_run_id: 'nr', attempt_id: 'a', payload: { unit_ref: 'refs/agentplane/r/units/n-0123456789abcdef', unit_commit: sha, tip_commit: sha, paths: ['a.txt'], omitted: 0 } },
      { ...base, type: 'workspace_released', sequence: 4, node_id: 'n', node_run_id: 'nr', payload: { action: 'retained', reason: 'conflict', path: '/w' } },
      { ...base, type: 'source_workspace_changed', sequence: 5, payload: { paths: [], omitted: 0, indeterminate: true } },
    ]
    const run = stream.reduce((acc, e) => reduce(acc, e), initialRun())
    expect(run.eventCount).toBe(5)
    expect(run.tail.map((e) => e.type)).toEqual(stream.map((e) => e.type))
    expect(run.tail.every((e) => e.kind === undefined)).toBe(true)
    expect(run.status).toBe(initialRun().status)
  })
  it('filters Failed, Retried and Human from the recorded runs', () => {
    const tail = reduceAll(reject).tail
    expect(filterEvents(tail, 'all')).toHaveLength(tail.length)
    expect(filterEvents(tail, 'human').map((e) => e.type)).toEqual(['approval_requested', 'approval_decided'])
    expect(filterEvents(tail, 'failed').some((e) => e.type === 'run_finished')).toBe(true)
    expect(filterEvents(reduceAll(approve).tail, 'retried').some((e) => e.type === 'attempt_started')).toBe(true)
    expect(filterEvents(reduceAll(approve).tail, 'failed').some((e) => e.type === 'gate_check')).toBe(true)
  })
})

describe('live and replay agree', () => {
  it('KPIs, timeline and event tail are identical when events arrive in batches, replayed, or duplicated', () => {
    const replay = reduceAll(approve)
    const live = reduceAll(approve.slice(10), reduceAll(approve.slice(0, 10)))
    const duplicated = reduceAll(approve, reduceAll(approve.slice(0, 15)))
    for (const other of [live, duplicated]) {
      expect(runKpis(other)).toEqual(runKpis(replay))
      expect(timeline(other)).toEqual(timeline(replay))
      expect(other.tail).toEqual(replay.tail)
    }
  })
})
