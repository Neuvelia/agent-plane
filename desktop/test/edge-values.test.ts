import { describe, expect, it } from 'vitest'
import valueEvents from './fixtures/demo-approve-values.json'
import type { ApEvent, RunSummary } from '../src/types'
import { choose, confirmVersion, defaultRun, noChoice, pickerRuns, readableRunId, resolveRun } from '../src/valuePicker'
import { FIRST_VALUE_SCHEMA, MAX_VALUES_BYTES, graphInputSlot, initialValues, outputSlot, reduceAllValues, reduceValues, valueNodeStatus, type RecordedValue, type ValueSlot } from '../src/values'

// demo-approve-values.json: recorded `agentplane run .agents/graphs/demo-vertical.yaml --approve approve
// --json --record-values` (event schema 10, ADR-0023). It has three node values and no graph input.
const stream = valueEvents as unknown as ApEvent[]
const RUN = stream[0]!.run_id

let seq = 0
function valueEvent(over: { run?: string; node?: string; nodeRun?: string; port: string; value?: string; schema?: number; sequence?: number; type?: string; truncated?: boolean }): ApEvent {
  const sequence = over.sequence ?? ++seq
  const nodeRun = over.nodeRun ?? (over.node ? `run-of-${over.node}` : undefined)
  return {
    event_schema_version: over.schema ?? 11,
    type: 'value_recorded',
    timestamp: '2026-11-17T10:00:00Z',
    sequence,
    run_id: over.run ?? 'r1',
    graph_id: 'g',
    graph_version: 'v',
    node_id: over.node,
    node_run_id: nodeRun,
    payload: { node_run_id: nodeRun, port: over.port, type: over.type ?? 'string', size_bytes: over.value?.length ?? 0, sha256: 'ab', truncated: over.truncated ?? false, redacted: false, ...(over.value === undefined ? {} : { value: over.value }) },
  }
}

const slotValue = (slot: ValueSlot | undefined): RecordedValue => {
  if (slot?.kind !== 'value') throw new Error(`expected a value, got ${slot?.kind}`)
  return slot.value
}

describe('values reducer', () => {
  it('keeps one slot per (node, port) and per graph input, with provenance', () => {
    const v = reduceAllValues(RUN, stream)
    expect(v.valueEvents).toBe(3)
    expect(slotValue(outputSlot(v, 'fast', 'stdout'))).toMatchObject({ port: 'stdout', type: 'string', value: 'fast branch done', nodeRunId: 'c7f4d909-3afe-4064-ab9b-720d941a7731', truncated: false, redacted: false })
    const json = slotValue(outputSlot(v, 'flaky', 'json'))
    expect(json.type).toBe('json')
    expect(json.attemptId).toBe('a6d4a2bc-cd3e-41aa-b48d-2c9f63cc8b99')
    expect(slotValue(outputSlot(v, 'approve', 'decision')).value).toBe('approved')
    // A node's output is not a graph input of the same name, and an unrecorded key has no slot.
    expect(graphInputSlot(v, 'stdout')).toBeUndefined()
    expect(outputSlot(v, 'fast', 'nothing')).toBeUndefined()
  })

  it('records a graph input under inputs.NAME with no node and no node run', () => {
    const v = reduceAllValues('r1', [valueEvent({ port: 'inputs.job', value: 'do it' }), valueEvent({ node: 'inputs', port: 'job', value: 'node named inputs' })])
    const input = slotValue(graphInputSlot(v, 'job'))
    expect(input).toMatchObject({ port: 'inputs.job', value: 'do it', nodeId: undefined, nodeRunId: undefined })
    // A node that happens to be named "inputs" never collides with a graph input.
    expect(slotValue(outputSlot(v, 'inputs', 'job')).value).toBe('node named inputs')
  })

  it('keeps an empty string as an empty value and a value-less event as a slot with no text', () => {
    const v = reduceAllValues('r1', [valueEvent({ node: 'a', port: 'empty', value: '' }), valueEvent({ node: 'a', port: 'gone', truncated: true })])
    expect(slotValue(outputSlot(v, 'a', 'empty')).value).toBe('')
    expect(slotValue(outputSlot(v, 'a', 'gone'))).toMatchObject({ value: undefined, truncated: true })
    expect(v.valueEvents).toBe(2)
  })

  it('ignores a duplicate sequence and the events of another run', () => {
    const e = valueEvent({ node: 'a', port: 'p', value: 'x', sequence: 5 })
    const once = reduceValues(initialValues('r1'), e)
    expect(reduceValues(once, e)).toBe(once)
    expect(reduceValues(once, valueEvent({ node: 'a', port: 'p', value: 'older', sequence: 3 }))).toBe(once)
    const other = reduceValues(once, valueEvent({ run: 'r2', node: 'a', port: 'q', value: 'y', sequence: 9 }))
    expect(other).toBe(once)
    expect(outputSlot(other, 'a', 'q')).toBeUndefined()
  })

  it('does not accept a value from a stream that claims a schema without values', () => {
    expect(FIRST_VALUE_SCHEMA).toBe(10)
    const v = reduceAllValues('r1', [valueEvent({ node: 'a', port: 'p', value: 'x', schema: 9 })])
    expect(v.valueEvents).toBe(0)
    expect(v.schemaVersion).toBe(9)
  })

  it('reports two node runs for one key as ambiguous, with both sequences, and shows neither value', () => {
    const v = reduceAllValues('r1', [
      valueEvent({ node: 'a', nodeRun: 'one', port: 'p', value: 'first', sequence: 4 }),
      valueEvent({ node: 'a', nodeRun: 'two', port: 'p', value: 'second', sequence: 9 }),
      valueEvent({ node: 'a', nodeRun: 'three', port: 'p', value: 'third', sequence: 12 }),
    ])
    expect(outputSlot(v, 'a', 'p')).toEqual({ kind: 'ambiguous', sequences: [4, 9] })
    expect(v.retainedBytes).toBe(0)
    expect(JSON.stringify(v)).not.toMatch(/first|second|third/)
  })

  it('treats a second event of the same node run as the same accepted value (immutable)', () => {
    const v = reduceAllValues('r1', [valueEvent({ node: 'a', port: 'p', value: 'first' }), valueEvent({ node: 'a', port: 'p', value: 'again' })])
    expect(slotValue(outputSlot(v, 'a', 'p')).value).toBe('first')
  })

  it('derives node status from node events only', () => {
    const v = reduceAllValues(RUN, stream.slice(0, 5))
    expect(valueNodeStatus(v, 'fast')).toBe('running')
    expect(valueNodeStatus(v, 'apply')).toBe('pending')
    const done = reduceAllValues(RUN, stream)
    expect(['fast', 'flaky', 'approve', 'apply'].map((n) => valueNodeStatus(done, n))).toEqual(['succeeded', 'succeeded', 'succeeded', 'succeeded'])
    expect(valueNodeStatus(reduceAllValues(RUN, stream.slice(0, 23)), 'approve')).toBe('waiting_human')
    expect(done.schemaVersion).toBe(10)
  })

  it('never mutates its input', () => {
    const before = reduceAllValues(RUN, stream.slice(0, 9))
    const snapshot = structuredClone(before)
    reduceAllValues(RUN, stream)
    reduceValues(before, stream[9]!)
    expect(before).toEqual(snapshot)
  })

  it('bounds retained text to 1 MiB for a 5 MiB stream and drops the rest of the text, not the facts', () => {
    const chunk = 'x'.repeat(128 * 1024)
    const events = Array.from({ length: 40 }, (_, i) => valueEvent({ node: `n${i}`, port: 'out', value: chunk }))
    const v = reduceAllValues('r1', events)
    expect(v.retainedBytes).toBeLessThanOrEqual(MAX_VALUES_BYTES)
    const kept = Object.values(v.slots).filter((s) => s.kind === 'value' && s.value.value !== undefined)
    expect(kept.length).toBe(MAX_VALUES_BYTES / chunk.length)
    const dropped = slotValue(outputSlot(v, 'n39', 'out'))
    expect(dropped).toMatchObject({ value: undefined, droppedLocally: true, sizeBytes: chunk.length })
    expect(v.valueEvents).toBe(40)
    const textBytes = Object.values(v.slots).reduce((n, s) => n + (s.kind === 'value' ? (s.value.value?.length ?? 0) : 0), 0)
    expect(textBytes).toBeLessThanOrEqual(MAX_VALUES_BYTES)
  })

  it('counts UTF-8 bytes, not characters, against the bound', () => {
    const wide = '€'.repeat(200 * 1024) // 3 bytes each: 600 KiB
    const v = reduceAllValues('r1', [valueEvent({ node: 'a', port: 'p', value: wide }), valueEvent({ node: 'b', port: 'p', value: wide })])
    expect(slotValue(outputSlot(v, 'a', 'p')).value).toBe(wide)
    expect(slotValue(outputSlot(v, 'b', 'p'))).toMatchObject({ value: undefined, droppedLocally: true })
  })
})

describe('replay equals live', () => {
  it('reducing the stream in one pass equals every split with a reconnect replay', () => {
    const once = reduceAllValues(RUN, stream)
    for (let i = 0; i <= stream.length; i++) {
      const head = reduceAllValues(RUN, stream.slice(0, i))
      // The reconnect replays the whole stream from sequence zero over the state already held.
      expect(stream.reduce(reduceValues, head), `split at ${i}`).toEqual(once)
      // Following live after the prefix is the same as replaying.
      expect(stream.slice(i).reduce(reduceValues, head), `follow at ${i}`).toEqual(once)
    }
  })
})

// ---- run picker ----

const run = (id: string, startedAt: string, version: string | undefined, over: Partial<RunSummary> = {}): RunSummary => ({
  run_id: id,
  graph_id: 'demo',
  started_at: startedAt,
  status: 'succeeded',
  event_count: 10,
  graph_version: version,
  ...over,
})

const runs: RunSummary[] = [
  run('old-same', '2026-11-10T10:00:00Z', 'v2'),
  run('new-other', '2026-11-16T10:00:00Z', 'v1'),
  run('new-same', '2026-11-15T10:00:00Z', 'v2'),
  run('no-version', '2026-11-17T10:00:00Z', undefined),
  run('elsewhere', '2026-11-18T10:00:00Z', 'v2', { graph_id: 'other' }),
  run('corrupt', '2026-11-18T11:00:00Z', 'v2', { error: 'corrupt' }),
]

describe('run picker', () => {
  it('lists only runs of the open graph id, newest first', () => {
    expect(pickerRuns(runs, 'demo', 'v2').map((r) => r.runId)).toEqual(['no-version', 'new-other', 'new-same', 'old-same'])
    expect(pickerRuns(runs, undefined, 'v2')).toEqual([])
  })

  it('marks version equality and tags a missing graph_version as different', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    expect(Object.fromEntries(list.map((r) => [r.runId, r.sameVersion]))).toEqual({ 'no-version': false, 'new-other': false, 'new-same': true, 'old-same': true })
    // A document with no known version (it does not parse) matches nothing.
    expect(pickerRuns(runs, 'demo', undefined).every((r) => !r.sameVersion)).toBe(true)
  })

  it('orders deterministically when start times are equal or unparseable', () => {
    expect(pickerRuns([run('a', 'same', 'v'), run('b', 'same', 'v')], 'demo', 'v').map((r) => r.runId)).toEqual(['b', 'a'])
    expect(pickerRuns([run('b', 'same', 'v'), run('a', 'same', 'v')], 'demo', 'v').map((r) => r.runId)).toEqual(['b', 'a'])
  })

  it('defaults to the newest run of the same version, even when a newer run is of another version', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    expect(defaultRun(list)?.runId).toBe('new-same')
    expect(resolveRun(list, noChoice)).toMatchObject({ run: { runId: 'new-same' }, needsConfirmation: false, differentVersion: false })
  })

  it('chooses nothing by itself when no run has the open version', () => {
    const list = pickerRuns(runs, 'demo', 'v9')
    expect(defaultRun(list)).toBeUndefined()
    expect(resolveRun(list, noChoice)).toEqual({ needsConfirmation: false, differentVersion: false })
    expect(readableRunId(resolveRun(list, noChoice))).toBeUndefined()
  })

  it('never makes a different-version run readable without an explicit confirmation', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    for (const id of ['new-other', 'no-version']) {
      const picked = resolveRun(list, choose(noChoice, id))
      expect(picked).toMatchObject({ needsConfirmation: true, differentVersion: false })
      expect(readableRunId(picked)).toBeUndefined()
    }
    const confirmed = resolveRun(list, confirmVersion(choose(noChoice, 'new-other'), 'new-other'))
    expect(confirmed).toMatchObject({ needsConfirmation: false, differentVersion: true })
    expect(readableRunId(confirmed)).toBe('new-other')
    // Confirming one run does not confirm another.
    expect(resolveRun(list, confirmVersion(choose(noChoice, 'no-version'), 'new-other')).needsConfirmation).toBe(true)
  })

  it('an explicit choice lasts and survives a new run appearing; a pruned choice falls back to the default', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    const choice = choose(noChoice, 'old-same')
    expect(resolveRun(list, choice).run?.runId).toBe('old-same')
    const later = pickerRuns([...runs, run('newest', '2026-11-18T12:00:00Z', 'v2')], 'demo', 'v2')
    expect(resolveRun(later, choice).run?.runId).toBe('old-same')
    expect(resolveRun(later, noChoice).run?.runId).toBe('newest')
    expect(resolveRun(pickerRuns(runs.filter((r) => r.run_id !== 'old-same'), 'demo', 'v2'), choice).run?.runId).toBe('new-same')
  })
})

