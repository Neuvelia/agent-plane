import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import rejectEvents from './fixtures/demo-reject.json'
import { MAX_LOG_CHARS, classifyEnd, streamLost, displayStatus, initialRun, nodeView, pendingApprovals, presentRun, reduce, reduceAll, settle } from '../src/reducer'
import type { ApEvent } from '../src/types'

// Both fixtures are real event streams recorded from `agentplane run
// .agents/graphs/demo-vertical.yaml --approve|--reject approve --json`.
const approve = approveEvents as unknown as ApEvent[]
const reject = rejectEvents as unknown as ApEvent[]

function firstIndex(events: ApEvent[], pred: (e: ApEvent) => boolean): number {
  const i = events.findIndex(pred)
  expect(i).toBeGreaterThanOrEqual(0)
  return i
}

describe('demo run reduced from events', () => {
  const run = reduceAll(approve)

  it('ends succeeded with every node succeeded', () => {
    expect(run.status).toBe('succeeded')
    expect(run.graphId).toBe('demo-vertical')
    expect(Object.values(run.nodes).map((n) => n.status)).toEqual(['succeeded', 'succeeded', 'succeeded', 'succeeded'])
    expect(run.eventCount).toBe(approve.length)
  })

  it('shows the two independent branches running at the same time', () => {
    let state = initialRun()
    let overlapped = false
    for (const e of approve) {
      state = reduce(state, e)
      const active = ['fast', 'flaky'].filter((id) => ['running', 'retrying'].includes(displayStatus(nodeView(state, id))))
      if (active.length === 2) overlapped = true
    }
    expect(overlapped).toBe(true)
  })

  it('retries only the failing node: two attempts, red then green, sibling untouched', () => {
    const flaky = run.nodes.flaky!
    expect(flaky.attempts.map((a) => a.outcome)).toEqual(['rejected', 'accepted'])
    expect(flaky.attempts.map((a) => a.check?.verdict)).toEqual(['red', 'green'])
    expect(flaky.attempts[0]!.check?.reason).toBe('predicate mismatch')
    expect(flaky.attempts[0]!.check?.checkId).toBe('flaky-ready')
    expect(run.nodes.fast!.attempts).toHaveLength(0)
    expect(run.nodes.apply!.attempts).toHaveLength(0)
  })

  it('attributes logs to the attempt that produced them', () => {
    const flaky = run.nodes.flaky!
    expect(flaky.attempts[0]!.log).toContain('{"ready":false}')
    expect(flaky.attempts[1]!.log).toContain('{"ready":true}')
    expect(flaky.log).toContain('false')
    expect(flaky.log).toContain('true')
    expect(run.nodes.fast!.log).toBe('fast branch done')
  })

  it('derives "retrying" from the second attempt, not from stored state', () => {
    const i = firstIndex(approve, (e) => e.type === 'attempt_started' && e.payload.number === 2)
    const state = reduceAll(approve.slice(0, i + 1))
    expect(displayStatus(nodeView(state, 'flaky'))).toBe('retrying')
    expect(state.nodes.flaky!.status).toBe('running')
  })

  it('waits for a human between approval_requested and approval_decided', () => {
    const requested = firstIndex(approve, (e) => e.type === 'approval_requested')
    const waiting = reduceAll(approve.slice(0, requested + 1))
    expect(waiting.nodes.approve!.status).toBe('waiting_human')
    expect(waiting.nodes.approve!.approval?.prompt).toContain('Approve applying')
    expect(waiting.nodes.approve!.approval?.context[0]?.name).toBe('summary')
    expect(pendingApprovals(waiting).map((n) => n.id)).toEqual(['approve'])
    expect(nodeView(waiting, 'apply').status).toBe('pending')
    expect(waiting.status).toBe('running')

    const decided = firstIndex(approve, (e) => e.type === 'approval_decided')
    const after = reduceAll(approve.slice(0, decided + 1))
    expect(after.nodes.approve!.approval?.decision).toBe('approved')
    expect(pendingApprovals(after)).toHaveLength(0)
  })
})

describe('rejected run', () => {
  const run = reduceAll(reject)
  it('fails the human node with a safe code and skips the dependent', () => {
    expect(run.status).toBe('failed')
    expect(run.nodes.approve!.status).toBe('failed')
    expect(run.nodes.approve!.failureKind).toBe('rejected')
    expect(run.nodes.approve!.diagnosticCodes).toContain('human_rejected')
    expect(run.nodes.approve!.approval?.decision).toBe('rejected')
    expect(run.nodes.apply!.status).toBe('skipped')
  })
})

describe('reducer properties', () => {
  it('is idempotent under replay and reconnect (duplicate events are ignored)', () => {
    const once = reduceAll(approve)
    const twice = reduceAll([...approve, ...approve])
    expect(twice).toEqual(once)
    const half = approve.length >> 1
    const resumed = reduceAll(approve, reduceAll(approve.slice(0, half)))
    expect(resumed).toEqual(once)
  })

  it('gives the same state for a stored replay and an incremental live stream', () => {
    let live = initialRun()
    for (const e of approve) live = reduce(live, e)
    expect(live).toEqual(reduceAll(approve))
  })

  it('never mutates its input state', () => {
    const before = reduceAll(approve.slice(0, 10))
    const snapshot = JSON.stringify(before)
    reduce(before, approve[10]!)
    expect(JSON.stringify(before)).toBe(snapshot)
  })

  it('ignores event types it does not know', () => {
    const unknown = { ...approve[1]!, type: 'future_event', sequence: 9999, payload: { x: 1 } }
    const state = reduce(reduceAll(approve), unknown)
    expect(state.status).toBe('succeeded')
    expect(state.lastSequence).toBe(9999)
  })

  it('reports a log without a terminal event as interrupted unless it is being followed', () => {
    const cut = reduceAll(approve.slice(0, 12))
    expect(cut.status).toBe('running')
    expect(settle(cut, false).status).toBe('interrupted')
    expect(settle(cut, true).status).toBe('running')
    expect(settle(reduceAll(approve), false).status).toBe('succeeded')
  })

  it('bounds retained log text', () => {
    const big = btoa('x'.repeat(40 * 1024))
    const base = approve.find((e) => e.type === 'log_chunk')!
    let state = initialRun()
    for (let i = 1; i <= 4; i++) {
      state = reduce(state, { ...base, sequence: i, payload: { stream: 'stdout', data: big } })
    }
    expect(state.nodes[base.node_id!]!.log.length).toBe(MAX_LOG_CHARS)
  })
})

describe('lost connection after the run already finished', () => {
  it('is not reported once the terminal event was received', () => {
    expect(streamLost(reduceAll(approve), 'dropped')).toBeNull()
    expect(streamLost(reduceAll(reject), 'dropped')).toBeNull()
  })
  it('is reported while the run is still open', () => {
    expect(streamLost(reduceAll(approve.slice(0, 12)), 'dropped')).toBe('dropped')
    expect(streamLost(reduceAll(approve.slice(0, 12)), null)).toBeNull()
  })
})

describe('stream end classification', () => {
  it('treats only the server end event as a complete run', () => {
    expect(classifyEnd('end')).toEqual({ kind: 'complete' })
  })
  it('reports everything else as a lost connection, never as an interrupted run', () => {
    for (const reason of ['closed', 'aborted', 'http 500', 'fetch failed', '']) {
      expect(classifyEnd(reason).kind).toBe('lost')
    }
    const storage = classifyEnd('server_error')
    expect(storage.kind === 'lost' && storage.message).toMatch(/could not be stored/)
    expect(storage.kind === 'lost' && storage.code).toBe('storage')
    const dropped = classifyEnd('eof')
    expect(dropped.kind === 'lost' && dropped.code).toBe('connection')
  })
  it('does not invent a terminal status when the connection is lost mid-run', () => {
    const cut = reduceAll(approve.slice(0, 12))
    expect(presentRun(cut, false, true).status).toBe('running') // lost: keep what the events said
    expect(presentRun(cut, false, false).status).toBe('running') // still following
    expect(presentRun(cut, true, false).status).toBe('interrupted') // ended normally, no terminal event
    expect(presentRun(reduceAll(approve), true, false).status).toBe('succeeded')
  })
})
