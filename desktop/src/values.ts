import type { ApEvent } from './types'

// The recorded values of one run (F2.3 requirements 15 and 19, ADR-0023).
//
// This is deliberately a second, small reducer next to src/reducer.ts: the run
// view never sees a value (schema10.test.ts), and this module never builds run
// state. It keeps only value_recorded events and the little the value states
// need (the event schema version and each node's status). Values are user data:
// they live in this state only, which a caller drops when the run, the document
// or the screen goes away. Nothing here stores, logs or sends them anywhere.

/** Schema in which value_recorded appeared; an older stream cannot carry values. */
export const FIRST_VALUE_SCHEMA = 10

/** The engine's per-run cap on recorded text (events.MaxRunValueBytes); the renderer never keeps more. */
export const MAX_VALUES_BYTES = 1024 * 1024

/** Distinct (node, port) keys kept. A graph that large does not exist; the bound keeps a hostile stream finite. */
export const MAX_VALUE_KEYS = 4096

export type ValueType = 'string' | 'number' | 'boolean' | 'json' | 'artifact_ref'

/** One recorded value, as the engine recorded it. `value` is absent when nothing could be carried. */
export interface RecordedValue {
  /** Output port name, or `inputs.NAME` for a graph input. */
  port: string
  /** Absent for a graph input. */
  nodeId?: string
  nodeRunId?: string
  attemptId?: string
  type: string
  sizeBytes: number
  sha256: string
  truncated: boolean
  redacted: boolean
  value?: string
  /** The renderer's own 1 MiB bound dropped the text of this value; the engine did not. */
  droppedLocally?: boolean
  sequence: number
}

/** The slot of one key: its value, or the fact that two node runs produced it. */
export type ValueSlot = { kind: 'value'; value: RecordedValue } | { kind: 'ambiguous'; sequences: [number, number] }

/** Node statuses the value states need, from node events only. Anything not listed is pending. */
export type ValueNodeStatus = 'running' | 'waiting_human' | 'succeeded' | 'failed' | 'skipped' | 'cancelled'

export interface RunValues {
  /** Events of any other run are ignored. */
  runId: string
  lastSequence: number
  /** Event schema of the stream; undefined until the first event. */
  schemaVersion?: number
  /** How many value_recorded events were seen, including ones that carried no text. */
  valueEvents: number
  nodeStatus: Readonly<Record<string, ValueNodeStatus>>
  slots: Readonly<Record<string, ValueSlot>>
  /** UTF-8 bytes of `value` text currently retained. */
  retainedBytes: number
  /** Keys beyond MAX_VALUE_KEYS were ignored. */
  keysOverflowed: boolean
}

export function initialValues(runId: string): RunValues {
  return { runId, lastSequence: 0, valueEvents: 0, nodeStatus: {}, slots: {}, retainedBytes: 0, keysOverflowed: false }
}

// A NUL cannot occur in a node ID or port name, so the key is unambiguous even
// for a node named "inputs".
const slotKey = (nodeId: string | undefined, port: string): string => `${nodeId ?? ''}\u0000${port}`

/** The slot of an output port of a node, if any event named it. */
export function outputSlot(values: RunValues, nodeId: string, port: string): ValueSlot | undefined {
  return values.slots[slotKey(nodeId, port)]
}

/** The slot of a graph input (key `inputs.NAME`). */
export function graphInputSlot(values: RunValues, name: string): ValueSlot | undefined {
  return values.slots[slotKey(undefined, `inputs.${name}`)]
}

/** The status the value states use for a node no event has started. */
export function valueNodeStatus(values: RunValues, nodeId: string): ValueNodeStatus | 'pending' {
  return values.nodeStatus[nodeId] ?? 'pending'
}

const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const num = (v: unknown): number => (typeof v === 'number' ? v : 0)
const utf8Bytes = (text: string): number => new TextEncoder().encode(text).length

function statusAfter(event: ApEvent): ValueNodeStatus | undefined {
  switch (event.type) {
    case 'node_started':
      return 'running'
    case 'approval_requested':
      return 'waiting_human'
    case 'approval_decided':
      return 'running'
    case 'node_finished':
    case 'node_terminated': {
      const s = str(event.payload.status)
      return s === 'succeeded' || s === 'failed' || s === 'skipped' || s === 'cancelled' ? s : 'failed'
    }
    default:
      return undefined
  }
}

function recordedFrom(event: ApEvent, retained: number): RecordedValue {
  const p = event.payload
  const text = typeof p.value === 'string' ? p.value : undefined
  const bytes = text === undefined ? 0 : utf8Bytes(text)
  const fits = text !== undefined && retained + bytes <= MAX_VALUES_BYTES
  return {
    port: str(p.port),
    nodeId: event.node_id,
    nodeRunId: str(p.node_run_id) || event.node_run_id || undefined,
    attemptId: str(p.attempt_id) || event.attempt_id || undefined,
    type: str(p.type),
    sizeBytes: num(p.size_bytes),
    sha256: str(p.sha256),
    truncated: p.truncated === true,
    redacted: p.redacted === true,
    value: fits ? text : undefined,
    droppedLocally: text !== undefined && !fits ? true : undefined,
    sequence: event.sequence,
  }
}

function withValue(previous: RunValues, event: ApEvent): RunValues {
  const port = str(event.payload.port)
  if (port === '') return previous
  const key = slotKey(event.node_id, port)
  const existing = previous.slots[key]
  const counted = previous.valueEvents + 1
  if (!existing && Object.keys(previous.slots).length >= MAX_VALUE_KEYS) return { ...previous, valueEvents: counted, keysOverflowed: true }
  const next = recordedFrom(event, previous.retainedBytes)
  if (!existing) {
    const added = next.value === undefined ? 0 : utf8Bytes(next.value)
    return { ...previous, valueEvents: counted, retainedBytes: previous.retainedBytes + added, slots: { ...previous.slots, [key]: { kind: 'value', value: next } } }
  }
  // Accepted output is immutable: a second event of the same node run changes nothing. A second node run
  // is a contradiction the inspector must show, never resolve (F2.3 requirement 19).
  if (existing.kind === 'ambiguous') return { ...previous, valueEvents: counted }
  if (existing.value.nodeRunId === next.nodeRunId) return { ...previous, valueEvents: counted }
  const slots = { ...previous.slots, [key]: { kind: 'ambiguous', sequences: [existing.value.sequence, next.sequence] } satisfies ValueSlot }
  // Neither value is shown, so neither is kept.
  return { ...previous, valueEvents: counted, slots, retainedBytes: previous.retainedBytes - (existing.value.value === undefined ? 0 : utf8Bytes(existing.value.value)) }
}

/**
 * reduceValues returns a new state and never mutates its input. An event that is
 * not newer than the last applied one, or that belongs to another run, is
 * ignored, so replay-then-follow and reconnects are idempotent (as in reduce).
 */
export function reduceValues(previous: RunValues, event: ApEvent): RunValues {
  if (event.run_id !== previous.runId || event.sequence <= previous.lastSequence) return previous
  const base: RunValues = { ...previous, lastSequence: event.sequence, schemaVersion: previous.schemaVersion ?? event.event_schema_version }
  if (event.type === 'value_recorded') {
    // A stream that claims schema 1-9 cannot carry values; the engine rejects it, and so do we.
    return event.event_schema_version >= FIRST_VALUE_SCHEMA ? withValue(base, event) : base
  }
  const status = statusAfter(event)
  if (status && event.node_id && base.nodeStatus[event.node_id] !== status) return { ...base, nodeStatus: { ...base.nodeStatus, [event.node_id]: status } }
  return base
}

export function reduceAllValues(runId: string, events: ApEvent[]): RunValues {
  return events.reduce(reduceValues, initialValues(runId))
}
