import { describe, expect, it } from 'vitest'
import { applyPage, applySnapshot, currentRunView, emptyRunView, type EventPage, type RunFact } from './runState'

const runID = '00000000-0000-4000-8000-000000000099'
const hashA = `sha256:${'a'.repeat(64)}`
const hashB = `sha256:${'b'.repeat(64)}`

function fact(sequence: number, type = 'run_started', timestamp = '2026-10-20T12:00:00Z'): RunFact {
  return { run_id: runID, sequence, type, timestamp, event_schema_version: 8, payload: {} }
}

function page(events: RunFact[], hashes: string[], after = 0, watermark = events.at(-1)?.sequence ?? 0): EventPage {
  const omissions: Array<{ from: number; to: number; reason: 'not_retained' }> = []
  let prior = after
  for (const event of events) {
    if (event.sequence > prior + 1) omissions.push({ from: prior + 1, to: event.sequence - 1, reason: 'not_retained' })
    prior = event.sequence
  }
  const nextAfter = events.at(-1)?.sequence ?? watermark
  if (nextAfter > prior) omissions.push({ from: prior + 1, to: nextAfter, reason: 'not_retained' })
  return {
    run_id: runID, after_sequence: after, events, event_hashes: hashes, omissions,
    next_after: nextAfter, has_more: false, watermark,
    stats: { retained_omitted_events: 0, retained_omitted_bytes: 0,
      live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false }, finished: false
  }
}

describe('retained run reducer', () => {
  it('orders by sequence, not timestamps, and ignores exact duplicate bytes', () => {
    let view = emptyRunView(runID)
    view = applyPage(view, page([fact(2, 'node_ready', '2026-10-20T10:00:00Z')], [hashB], 1, 2))
    view = applyPage(view, page([fact(1, 'run_started', '2026-10-20T15:00:00Z'), fact(2, 'node_ready', '2026-10-20T10:00:00Z')], [hashA, hashB], 0, 2))
    expect(view.events.map((event) => event.sequence)).toEqual([1, 2])
    expect(view.conflicts).toEqual([])
  })

  it('reports conflicting bytes for one sequence without replacing accepted fact', () => {
    let view = applyPage(emptyRunView(runID), page([fact(1)], [hashA]))
    view = applyPage(view, page([fact(1, 'run_finished')], [hashB]))
    expect(view.events[0].type).toBe('run_started')
    expect(view.conflicts).toEqual([1])
  })

  it('keeps omissions explicit and takes terminal state only from snapshot', () => {
    let view = applyPage(emptyRunView(runID), {
      ...page([fact(1), fact(3, 'run_finished')], [hashA, hashB], 0, 3), finished: true,
      stats: { retained_omitted_events: 1, retained_omitted_bytes: 42,
        live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false }
    })
    expect(view.finished).toBe(false)
    expect(view.omitted.retained_omitted_events).toBe(1)
    view = applySnapshot(view, { run_id: runID, state: 'unknown', finished: false, trace_watermark: 3 })
    expect(view.state).toBe('unknown')
    view = applySnapshot(view, { run_id: runID, state: 'succeeded', finished: true, trace_watermark: 3 })
    expect(view.finished).toBe(true)
    expect(view.state).toBe('succeeded')
  })

  it('keeps live history availability separate from runtime success', () => {
    let view = applySnapshot(emptyRunView(runID), { run_id: runID, state: 'running', finished: false,
      trace_watermark: 1, history_availability: 1 })
    view = applySnapshot(view, { run_id: runID, state: 'succeeded', finished: true,
      trace_watermark: 2, history_availability: 2 })
    expect(view.state).toBe('succeeded')
    expect(view.historyAvailability).toBe(2)
    expect(() => applySnapshot(view, { run_id: runID, state: 'succeeded', finished: true,
      trace_watermark: 2, history_availability: 4 as 1 })).toThrow()
  })

  it('fails closed on unsafe JS integers and inconsistent page identity', () => {
    expect(() => applyPage(emptyRunView(runID), page([fact(2 ** 53)], [hashA], 0, 2 ** 53))).toThrow()
    expect(() => applyPage(emptyRunView(runID), page([fact(1)], []))).toThrow()
    expect(() => applyPage(emptyRunView(runID), { ...page([fact(1)], [hashA]), run_id: 'other' })).toThrow()
  })

  it('advances through an all-omission page without fabricating an event', () => {
    let view = applyPage(emptyRunView(runID), { ...page([fact(1)], [hashA], 0, 3), has_more: true })
    view = applyPage(view, { ...page([], [], 1, 3), omissions: [
      { from: 2, to: 3, reason: 'sensitive_identity' }
    ] })
    expect(view.cursor).toBe(3)
    expect(view.events.map((event) => event.sequence)).toEqual([1])
    expect(view.omissionRanges).toEqual([{ from: 2, to: 3, reason: 'sensitive_identity' }])
  })

  it('rejects an unexplained or overlapping sequence gap', () => {
    expect(() => applyPage(emptyRunView(runID), { ...page([], [], 0, 2), omissions: [] })).toThrow()
    expect(() => applyPage(emptyRunView(runID), { ...page([fact(2)], [hashA], 0, 2),
      omissions: [{ from: 1, to: 2, reason: 'not_retained' }] })).toThrow()
  })

  it('does not reveal a fact after its sequence was marked sensitive', () => {
    let view = applyPage(emptyRunView(runID), { ...page([], [], 0, 1),
      omissions: [{ from: 1, to: 1, reason: 'sensitive_identity' }] })
    view = applyPage(view, { ...page([fact(1)], [hashA], 0, 1), omissions: [] })
    expect(view.events).toEqual([])
    expect(view.conflicts).toEqual([1])
  })

  it('marks a later omission overlapping an accepted fact as a conflict', () => {
    let view = applyPage(emptyRunView(runID), page([fact(1)], [hashA]))
    view = applyPage(view, { ...page([], [], 0, 1),
      omissions: [{ from: 1, to: 1, reason: 'not_retained' }] })
    expect(view.events.map((event) => event.sequence)).toEqual([1])
    expect(view.conflicts).toEqual([1])
  })
})

it('does not display a prior run replay while a new run loads', () => {
  const old = emptyRunView('old-run')
  expect(currentRunView(old, 'new-run')).toBeNull()
  expect(currentRunView(old, 'old-run')).toBe(old)
})
