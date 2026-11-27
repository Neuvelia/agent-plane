import type { Badges } from './issues'
import { makeRef, parseRef } from './layout'
import type { Definition, FieldDescriptor, NodeTypeDescriptor } from './types'

// What a step card says about itself (graph editor v2): its prompt, a couple of
// key settings, where each input comes from and what it puts out. Everything is
// read from the definition, the sidecar's field descriptors and the engine's
// diagnostics; nothing here branches on a node type or judges a value. A card is
// a summary: the inspector stays the place to edit.

export interface CardSetting {
  /** Descriptor id, for the field's label in the active language. */
  id: string
  key: string
  value: string
}

export type InputSource = { kind: 'node'; node: string; port: string } | { kind: 'graph'; name: string } | { kind: 'none' }

export interface CardInput {
  name: string
  required: boolean
  source: InputSource
  /** The engine reported an error on this input. */
  error: boolean
}

export interface CardOutput {
  name: string
  type: string
  /** The engine reported a warning (and no error) on this output, e.g. "not used". */
  warning: boolean
  error: boolean
}

export interface CardModel {
  prompt?: string
  settings: CardSetting[]
  inputs: CardInput[]
  outputs: CardOutput[]
  /** Maximum attempts of the node's correction loop, when it has one. */
  retries?: number
}

const MAX_SETTINGS = 2
const MAX_PROMPT = 400

const scalar = (v: unknown): string | undefined => (typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : undefined)

function settingValue(f: FieldDescriptor, v: unknown): string | undefined {
  if (f.kind === 'path_list' && Array.isArray(v)) {
    const items = v.filter((x): x is string => typeof x === 'string')
    return items.length > 0 ? items.join(', ') : undefined
  }
  return scalar(v)
}

/** Path lists first (the scope a step may touch), then required single values, in descriptor order. */
function keySettings(desc: NodeTypeDescriptor | undefined, config: Record<string, unknown>): CardSetting[] {
  if (!desc) return []
  const candidates = desc.config.filter((f) => f.key && (f.kind === 'path_list' || (f.required && (f.kind === 'string' || f.kind === 'enum'))))
  const ordered = [...candidates.filter((f) => f.kind === 'path_list'), ...candidates.filter((f) => f.kind !== 'path_list')]
  const out: CardSetting[] = []
  for (const f of ordered) {
    const value = settingValue(f, config[f.key!])
    if (value === undefined || value === '') continue
    out.push({ id: f.id, key: f.key!, value })
    if (out.length === MAX_SETTINGS) break
  }
  return out
}

function promptOf(desc: NodeTypeDescriptor | undefined, config: Record<string, unknown>): string | undefined {
  const f = desc?.config.find((d) => d.kind === 'text' && d.key && typeof config[d.key] === 'string' && (config[d.key] as string).trim() !== '')
  if (!f) return undefined
  const text = (config[f.key!] as string).trim()
  return text.length > MAX_PROMPT ? `${text.slice(0, MAX_PROMPT)}…` : text
}

function sourceOf(def: Definition, ref: string): InputSource {
  const edge = def.edges.find((e) => e.to === ref)
  const from = edge ? parseRef(edge.from) : null
  if (from) return { kind: 'node', node: from.node, port: from.port }
  const binding = (def.graph_input_bindings ?? []).find((b) => b.to === ref)
  if (binding) return { kind: 'graph', name: binding.from.replace(/^inputs\./, '') }
  return { kind: 'none' }
}

export function cardModel(def: Definition, id: string, desc: NodeTypeDescriptor | undefined, badges: Badges): CardModel {
  const node = def.nodes[id]
  if (!node) return { settings: [], inputs: [], outputs: [] }
  const config = node.config ?? {}
  const inputs = Object.entries(node.inputs ?? {}).map(([name, p]): CardInput => {
    const ref = makeRef(id, 'inputs', name)
    return { name, required: p.required === true, source: sourceOf(def, ref), error: (badges.ports[ref]?.errors ?? 0) > 0 }
  })
  const outputs = Object.entries(node.outputs ?? {}).map(([name, p]): CardOutput => {
    const b = badges.ports[makeRef(id, 'outputs', name)]
    return { name, type: p.type, error: (b?.errors ?? 0) > 0, warning: (b?.errors ?? 0) === 0 && (b?.warnings ?? 0) > 0 }
  })
  return { prompt: promptOf(desc, config), settings: keySettings(desc, config), inputs, outputs, retries: node.loop?.max_attempts }
}
