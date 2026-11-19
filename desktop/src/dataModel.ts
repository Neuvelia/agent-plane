import type { GraphSummary } from './types'
import { FIRST_VALUE_SCHEMA, graphInputSlot, outputSlot, valueNodeStatus, type RecordedValue, type RunValues } from './values'

// The Data tab's model (F2.7 FR11-13): for each declared input and output of a node,
// exactly one state, derived from the recorded source-side values (src/values.ts) and
// the declared bindings. A binding does not transform a value, so the "resolved
// input" of a node is the recorded value of its source. States are not folded
// together: each answers a different question (F2.3 requirement 17).

export type PortState =
  | { kind: 'bindingsUnavailable' }
  | { kind: 'optionalUnbound' }
  | { kind: 'unbound' }
  | { kind: 'olderRun' }
  | { kind: 'notRecorded' }
  | { kind: 'notFinished' }
  | { kind: 'failedOrSkipped' }
  | { kind: 'noValue' }
  | { kind: 'ambiguous'; sequences: [number, number] }
  | { kind: 'notCarried'; value: RecordedValue }
  | { kind: 'recorded'; value: RecordedValue }

export interface DataPort {
  name: string
  type: string
  required: boolean
  /** Where the value comes from: `inputs.NAME` or `node.port`. Absent for an output and for an unbound input. */
  source?: string
  state: PortState
}

export interface NodeData {
  /** The graph summary has no port or binding data (an older engine, or a graph that did not parse). */
  unavailable: boolean
  inputs: DataPort[]
  outputs: DataPort[]
}

type Source = { node: string; port: string } | { input: string }

function stateOfSource(values: RunValues, source: Source): PortState {
  if ('node' in source) {
    const status = valueNodeStatus(values, source.node)
    if (status === 'pending' || status === 'running' || status === 'waiting_human') return { kind: 'notFinished' }
    if (status !== 'succeeded') return { kind: 'failedOrSkipped' }
  }
  if (values.schemaVersion !== undefined && values.schemaVersion < FIRST_VALUE_SCHEMA) return { kind: 'olderRun' }
  if (values.valueEvents === 0) return { kind: 'notRecorded' }
  const slot = 'node' in source ? outputSlot(values, source.node, source.port) : graphInputSlot(values, source.input)
  if (!slot) return { kind: 'noValue' }
  if (slot.kind === 'ambiguous') return { kind: 'ambiguous', sequences: slot.sequences }
  return slot.value.value === undefined ? { kind: 'notCarried', value: slot.value } : { kind: 'recorded', value: slot.value }
}

const label = (source: Source): string => ('node' in source ? `${source.node}.${source.port}` : `inputs.${source.input}`)

/**
 * nodeData reads one node's declared ports and bindings from the graph summary and
 * resolves each against the recorded values. It never invents a value: anything the
 * stream does not carry is a named state.
 */
export function nodeData(values: RunValues, graph: GraphSummary | null, nodeId: string): NodeData {
  const declared = graph?.nodes.find((n) => n.id === nodeId)
  if (!declared?.inputs || !declared.outputs) return { unavailable: true, inputs: [], outputs: [] }
  const bindings = graph?.bindings
  const inputs = declared.inputs.map<DataPort>((p) => {
    const port = { name: p.name, type: p.type, required: p.required }
    if (!bindings) return { ...port, state: { kind: 'bindingsUnavailable' } }
    const bound = bindings.find((b) => b.target.node === nodeId && b.target.input === p.name)
    if (!bound) return { ...port, state: { kind: p.required ? 'unbound' : 'optionalUnbound' } }
    return { ...port, source: label(bound.source), state: stateOfSource(values, bound.source) }
  })
  const outputs = declared.outputs.map<DataPort>((p) => ({ name: p.name, type: p.type, required: p.required, state: stateOfSource(values, { node: nodeId, port: p.name }) }))
  return { unavailable: false, inputs, outputs }
}
