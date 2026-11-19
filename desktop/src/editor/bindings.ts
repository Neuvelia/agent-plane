import type { Diagnostic } from '../types'
import { parseRef } from './layout'
import type { Definition, Op } from './types'

// The Bindings panel's model (F2.3 E5a.2). It lists the bindings the engine
// returned, reads the diagnostics' own element indexes for the badges and turns
// a choice into operations. Whether two ports may be joined is the engine's
// answer to `list_connections` or to the batch itself; nothing here decides it.

/** A node-to-node edge, or a graph input bound to a node input. */
export type BindingKind = 'edge' | 'binding'

export interface Binding {
  kind: BindingKind
  from: string
  to: string
}

export interface BindingRow extends Binding {
  /** Position in `edges` or `graph_input_bindings`, which the diagnostics index. */
  index: number
  /** The declared type of the source port; empty when the source is not declared. */
  type: string
  errors: number
  warnings: number
}

/** `inputs.NAME` names a graph input; anything else is `NODE.outputs.PORT`. */
export const isGraphInput = (ref: string): boolean => ref.startsWith('inputs.')

export const kindOf = (from: string): BindingKind => (isGraphInput(from) ? 'binding' : 'edge')

const before = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)

/** Declared type of an output or graph input reference, from the definition. */
export function sourceType(def: Definition, from: string): string {
  if (isGraphInput(from)) return def.inputs?.[from.slice('inputs.'.length)]?.type ?? ''
  const ref = parseRef(from)
  return (ref && def.nodes[ref.node]?.outputs?.[ref.port]?.type) || ''
}

/** Declared type of a node input reference. */
export function targetType(def: Definition, to: string): string {
  const ref = parseRef(to)
  return (ref && def.nodes[ref.node]?.inputs?.[ref.port]?.type) || ''
}

function countAt(diags: Diagnostic[], key: 'edge_index' | 'graph_input_binding_index', index: number) {
  const here = diags.filter((d) => d.location?.[key] === index)
  const errors = here.filter((d) => d.severity === 'error').length
  return { errors, warnings: here.length - errors }
}

/**
 * Every edge and graph input binding as a row, ordered by target node, then
 * target input, then source, so the order is stable while the file is edited.
 */
export function bindingRows(def: Definition, diags: Diagnostic[]): BindingRow[] {
  const rows: BindingRow[] = [
    ...def.edges.map((e, index): BindingRow => ({ kind: 'edge', from: e.from, to: e.to, index, type: sourceType(def, e.from), ...countAt(diags, 'edge_index', index) })),
    ...(def.graph_input_bindings ?? []).map((b, index): BindingRow => ({ kind: 'binding', from: b.from, to: b.to, index, type: sourceType(def, b.from), ...countAt(diags, 'graph_input_binding_index', index) })),
  ]
  const target = (r: BindingRow) => parseRef(r.to)
  return rows.sort((a, b) => before(target(a)?.node ?? a.to, target(b)?.node ?? b.to) || before(target(a)?.port ?? '', target(b)?.port ?? '') || before(a.from, b.from) || before(a.kind, b.kind))
}

export const addOp = (from: string, to: string): Op => (isGraphInput(from) ? { op: 'add_graph_input_binding', from, to } : { op: 'add_edge', from, to })

export const removeOp = (b: Binding): Op => (b.kind === 'binding' ? { op: 'remove_graph_input_binding', from: b.from, to: b.to } : { op: 'remove_edge', from: b.from, to: b.to })

/** What currently feeds a node input, in file order: edges, then graph input bindings. */
export function providersOf(def: Definition, to: string): Binding[] {
  return [...def.edges.filter((e) => e.to === to).map((e): Binding => ({ kind: 'edge', from: e.from, to })), ...(def.graph_input_bindings ?? []).filter((b) => b.to === to).map((b): Binding => ({ kind: 'binding', from: b.from, to }))]
}

/**
 * The atomic batch that replaces what feeds `to` with `from`: remove the old
 * provider, add the new one. The engine refuses the whole batch if the new
 * connection is refused, so a failed replace keeps the old provider. Null when
 * nothing feeds the input (that is a plain add).
 */
export function replaceOps(def: Definition, from: string, to: string): Op[] | null {
  const old = providersOf(def, to)
  return old.length === 0 ? null : [...old.map(removeOp), addOp(from, to)]
}

export interface SourceChoice {
  ref: string
  type: string
}

/** Every source a binding can start from: graph inputs first, then node outputs by node and port. */
export function sourceChoices(def: Definition): SourceChoice[] {
  const inputs = Object.keys(def.inputs ?? {})
    .sort(before)
    .map((name): SourceChoice => ({ ref: `inputs.${name}`, type: def.inputs![name]!.type }))
  const outputs = Object.keys(def.nodes)
    .sort(before)
    .flatMap((id) =>
      Object.keys(def.nodes[id]!.outputs ?? {})
        .sort(before)
        .map((port): SourceChoice => ({ ref: `${id}.outputs.${port}`, type: def.nodes[id]!.outputs![port]!.type })),
    )
  return [...inputs, ...outputs]
}
