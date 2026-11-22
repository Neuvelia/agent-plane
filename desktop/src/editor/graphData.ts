import { addOp } from './bindings'
import type { Binding } from './bindings'
import type { Definition, Op } from './types'

// The model of the Graph inputs and Graph outputs cards (F2.3 E5a.3). It lists
// what the engine returned and builds operations; whether a retype, a binding or
// an output source is valid is the engine's answer to the operation itself.

export interface GraphInputRow {
  name: string
  type: string
  required: boolean
  /** Bindings from this input to node inputs, in file order. */
  bindings: Binding[]
}

const before = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)

/** Every declared graph input by name, with the bindings that start at it. */
export function graphInputRows(def: Definition): GraphInputRow[] {
  return Object.keys(def.inputs ?? {})
    .sort(before)
    .map((name) => ({
      name,
      type: def.inputs![name]!.type,
      required: def.inputs![name]!.required === true,
      bindings: (def.graph_input_bindings ?? []).filter((b) => b.from === `inputs.${name}`).map((b): Binding => ({ kind: 'binding', from: b.from, to: b.to })),
    }))
}

/** `topic`, then `topic_2`, `topic_3`, ...: the port's name, with the lowest free numeric suffix when it is taken. */
export function suggestInputName(port: string, taken: string[]): string {
  const used = new Set(taken)
  if (!used.has(port)) return port
  for (let n = 2; ; n++) if (!used.has(`${port}_${n}`)) return `${port}_${n}`
}

/**
 * The one batch behind "Add graph input" on an unbound required input: declare a
 * required input named after the port, with the port's type, and bind it.
 */
export function addInputFor(def: Definition, to: string, type: string): Op[] {
  const name = suggestInputName(to.slice(to.lastIndexOf('.') + 1), Object.keys(def.inputs ?? {}))
  return [{ op: 'add_graph_input', id: name, type, required: true }, addOp(`inputs.${name}`, to)]
}

export interface OutputRow {
  name: string
  from: string
  /** The source port exists and is not required: the output stays as written but is not offered again (decision 8). */
  optionalSource: boolean
}

export function outputRows(def: Definition): OutputRow[] {
  return Object.keys(def.outputs)
    .sort(before)
    .map((name) => {
      const from = def.outputs[name]!.from
      const [node, , port] = from.split('.')
      return { name, from, optionalSource: !!node && !!port && !!def.nodes[node]?.outputs?.[port] && def.nodes[node]!.outputs![port]!.required !== true }
    })
}

/** What a graph output may be set to: the required node outputs only (decision 8), by node and port. */
export function outputChoices(def: Definition): string[] {
  return Object.keys(def.nodes)
    .sort(before)
    .flatMap((id) =>
      Object.keys(def.nodes[id]!.outputs ?? {})
        .sort(before)
        .filter((port) => def.nodes[id]!.outputs![port]!.required === true)
        .map((port) => `${id}.outputs.${port}`),
    )
}

/** The choices for an existing output: the required ones, plus its own source shown as written when that is optional. */
export function outputOptions(def: Definition, current: string): string[] {
  const choices = outputChoices(def)
  return choices.includes(current) ? choices : [current, ...choices]
}
