import { FIRST_VALUE_SCHEMA, graphInputSlot, outputSlot, valueNodeStatus, type RecordedValue, type ValueNodeStatus } from '../values'
import type { PollStatus } from '../hooks'
import type { PickerRun, ResolvedRun } from '../valuePickerModel'
import type { RunValuesStream } from '../useRunValues'
import { parseRef } from './layout'
import { isGraphInput } from './bindings'

// What the edge inspector says about the value on one edge or graph input binding
// (F2.3 requirement 17). A pure function of the run list, the person's run choice
// and the values reducer: it reads facts the engine recorded and decides nothing
// about the graph. The order of the checks below IS the precedence of the spec.

/** The ten states of requirement 17, plus the two that are not a verdict yet. */
export type EdgeValueView =
  | { state: 'loading' }
  | { state: 'no_run' }
  | { state: 'run_unavailable' }
  | { state: 'different_version'; chosen: boolean }
  | { state: 'node_not_finished'; status: 'pending' | 'running' | 'waiting_human' }
  | { state: 'node_failed_or_skipped'; status: Extract<ValueNodeStatus, 'failed' | 'skipped' | 'cancelled'> }
  | { state: 'older_run'; schemaVersion: number }
  | { state: 'not_recorded' }
  | { state: 'no_value_for_port' }
  | { state: 'ambiguous'; sequences: [number, number] }
  | { state: 'value_not_carried'; value: RecordedValue }
  | { state: 'value'; value: RecordedValue }

export type EdgeValueState = EdgeValueView['state']

/** Every state a person can reach, in the order of precedence (requirement 17). */
export const EDGE_VALUE_STATES = [
  'no_run',
  'run_unavailable',
  'different_version',
  'node_not_finished',
  'node_failed_or_skipped',
  'older_run',
  'not_recorded',
  'no_value_for_port',
  'value_not_carried',
  'value',
] as const satisfies readonly EdgeValueState[]

export interface EdgeValueInput {
  /** The runs of this graph, newest first (valuePickerModel.ts). */
  runs: readonly PickerRun[]
  runsStatus: PollStatus
  resolved: ResolvedRun
  stream: RunValuesStream
  /** The source of the selected edge: `NODE.outputs.PORT` or `inputs.NAME`. */
  from: string
}

const PENDING = new Set(['pending', 'running', 'waiting_human'])

function runView(input: EdgeValueInput): EdgeValueView | null {
  const { runs, runsStatus, resolved, stream } = input
  if (runs.length === 0) return { state: runsStatus === 'loading' ? 'loading' : runsStatus === 'error' ? 'run_unavailable' : 'no_run' }
  if (stream.load === 'unavailable') return { state: 'run_unavailable' }
  // No run of this version, or one of another version nobody confirmed: nothing was read.
  if (!resolved.run || resolved.needsConfirmation) return { state: 'different_version', chosen: resolved.run !== undefined }
  if (!stream.values || stream.values.lastSequence === 0) return stream.load === 'ready' ? { state: 'run_unavailable' } : { state: 'loading' }
  return null
}

/** edgeValueView chooses the one state the inspector shows for the source `from`. */
export function edgeValueView(input: EdgeValueInput): EdgeValueView {
  const early = runView(input)
  if (early) return early
  const values = input.stream.values!
  const fromInput = isGraphInput(input.from)
  const output = fromInput ? null : parseRef(input.from)
  const slot = fromInput ? graphInputSlot(values, input.from.slice('inputs.'.length)) : output ? outputSlot(values, output.node, output.port) : undefined
  if (slot?.kind === 'ambiguous') return { state: 'ambiguous', sequences: slot.sequences }
  if (slot) return slot.value.value === undefined ? { state: 'value_not_carried', value: slot.value } : { state: 'value', value: slot.value }
  if (output) {
    const status = valueNodeStatus(values, output.node)
    if (PENDING.has(status)) return { state: 'node_not_finished', status: status as 'pending' | 'running' | 'waiting_human' }
    if (status === 'failed' || status === 'skipped' || status === 'cancelled') return { state: 'node_failed_or_skipped', status }
  }
  if (values.schemaVersion !== undefined && values.schemaVersion < FIRST_VALUE_SCHEMA) return { state: 'older_run', schemaVersion: values.schemaVersion }
  return { state: values.valueEvents === 0 ? 'not_recorded' : 'no_value_for_port' }
}
