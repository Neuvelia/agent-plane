import { describe, expect, it } from 'vitest'
import { emptyRunView } from './runState'
import { replayRun } from './replay'

const runID = '00000000-0000-4000-8000-000000000099'
const omissions = { retained_omitted_events: 1, retained_omitted_bytes: 10,
  live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false }

describe('renderer replay', () => {
  it('resumes from service cursor and reconciles terminal snapshot after pages', async () => {
    const cursors: number[] = []
    let snapshots = 0
    const view = await replayRun({
      snapshotRun: async () => {
        snapshots++
        return { run_id: runID, state: snapshots === 1 ? 'running' : 'succeeded',
          finished: snapshots === 2, trace_watermark: 3 }
      },
      eventsRun: async (_id, after) => {
        cursors.push(after)
        return { run_id: runID, after_sequence: after,
          events: after === 0 ? [{ run_id: runID, sequence: 1, type: 'run_started',
            timestamp: '2026-10-20T12:00:00Z', event_schema_version: 8, payload: {} }] : [],
          event_hashes: after === 0 ? [`sha256:${'a'.repeat(64)}`] : [],
          omissions: after === 0 ? [] : [{ from: 2, to: 3, reason: 'not_retained' }],
          next_after: after === 0 ? 1 : 3, has_more: after === 0,
          watermark: 3, stats: omissions, finished: true }
      }
    }, emptyRunView(runID))
    expect(cursors).toEqual([0, 1])
    expect(view.cursor).toBe(3)
    expect(view.events).toHaveLength(1)
    expect(view.omitted.retained_omitted_events).toBe(1)
    expect(view.state).toBe('succeeded')
    expect(view.finished).toBe(true)
  })
})
