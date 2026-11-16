import { describe, expect, it } from 'vitest'
import valueEvents from './fixtures/demo-approve-values.json'
import { reduceAll } from '../src/reducer'
import { filterEvents, runKpis, timeline } from '../src/insight'
import type { ApEvent } from '../src/types'

// Recorded from `agentplane run .agents/graphs/demo-vertical.yaml --approve approve
// --json --record-values` (event schema 10, ADR-0023).
const withValues = valueEvents as unknown as ApEvent[]
const withoutValues = withValues.filter((e) => e.type !== 'value_recorded')
const valueCount = withValues.length - withoutValues.length

describe('event schema 10 with recorded values', () => {
  it('is a real schema-10 stream that carries values', () => {
    expect(withValues.every((e) => e.event_schema_version === 10)).toBe(true)
    expect(valueCount).toBe(3)
  })

  it('reduces without error to the same run views as the stream without values', () => {
    const a = reduceAll(withValues)
    const b = reduceAll(withoutValues)
    expect(a.status).toBe('succeeded')
    expect(a.eventCount).toBe(b.eventCount + valueCount)
    expect({ ...a, eventCount: 0 }).toEqual({ ...b, eventCount: 0 })
    expect(runKpis(a)).toEqual(runKpis(b))
    expect(timeline(a)).toEqual(timeline(b))
    for (const f of ['all', 'failed', 'retried', 'human'] as const) expect(filterEvents(a.tail, f)).toEqual(filterEvents(b.tail, f))
  })

  it('never puts a recorded value into the event log or node views', () => {
    const run = reduceAll(withValues)
    expect(run.tail.some((r) => r.type === 'value_recorded')).toBe(false)
    // "fast branch done" is a recorded value and also fast's log; the value event must not add a second copy.
    expect(run.nodes.fast!.log).toBe('fast branch done')
    expect(JSON.stringify(run.tail)).not.toContain('fast branch done')
  })
})
