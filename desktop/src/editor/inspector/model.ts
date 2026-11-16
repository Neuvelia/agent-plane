import type { Diagnostic } from '../../types'
import type { Definition, EdgeDef, FieldDescriptor, JsonValue, NodeDef, NodeTypeDescriptor, Op, Rule } from '../types'

// The inspector's pure model (F2.2). It reads the sidecar's descriptors and the
// definition the engine returned and turns edits into operations. It decides no
// graph rule: which keys apply is read from the descriptors' rules, whether a
// value is valid is the engine's diagnostic, and what a port change touches is
// the engine's answer to a preview of the operation.

/** A key rule holds when the key is present (and has the value, when one is named). Input rules never hide keys, as in Go. */
export function holds(rules: Rule[] | undefined, config: Record<string, unknown>): boolean {
  return (rules ?? []).every((r) => {
    if (!r.key) return true
    if (!(r.key in config)) return false
    if (r.equals === undefined || r.equals === '') return true
    const v = config[r.key]
    return typeof v === 'string' ? v === r.equals : typeof v === 'boolean' ? String(v) === r.equals : false
  })
}

/** The keyed fields that apply to config, in descriptor order (the descriptors' own selection, mirrored from internal/fields). */
export function activeFields(fs: FieldDescriptor[], config: Record<string, unknown>): FieldDescriptor[] {
  const out: FieldDescriptor[] = []
  const walk = (list: FieldDescriptor[]) => {
    for (const f of list) {
      if (f.kind === 'group' && !f.key) {
        if (holds(f.requires, config)) walk(f.fields ?? [])
        continue
      }
      out.push(f)
    }
  }
  walk(fs)
  return out
}

// ---- diagnostics by path -------------------------------------------------------

const under = (path: string | undefined, prefix: string) => path === prefix || (!!path && (path.startsWith(prefix + '.') || path.startsWith(prefix + '[')))

/** Diagnostics whose path is the element or inside it. */
export const diagnosticsAt = (diags: Diagnostic[], prefix: string): Diagnostic[] => diags.filter((d) => under(d.location?.path, prefix))

/** Diagnostics whose path is exactly the element (for example an executor's `nodes.ID.config`). */
export const diagnosticsExactly = (diags: Diagnostic[], path: string): Diagnostic[] => diags.filter((d) => d.location?.path === path)

/** Every diagnostic that names the node, by id or by path. */
export const nodeDiagnostics = (diags: Diagnostic[], id: string): Diagnostic[] => diags.filter((d) => d.location?.node_id === id || under(d.location?.path, `nodes.${id}`))

export const configPath = (node: string, key: string) => `nodes.${node}.config.${key}`

// ---- input sources ---------------------------------------------------------------

export type Source = { kind: 'edge'; from: string } | { kind: 'graph'; from: string } | { kind: 'literal'; key: string } | { kind: 'unbound' }

/**
 * Where each declared input comes from, as declared (F2.2 requirement 9): an
 * edge, a graph input binding, a config value that stands in for the input
 * (a descriptor that excludes the input, such as an agent's `task`), or nothing.
 */
export function inputSources(def: Definition, node: string, desc?: NodeTypeDescriptor): Record<string, Source[]> {
  const n = def.nodes[node]
  const out: Record<string, Source[]> = {}
  for (const port of Object.keys(n?.inputs ?? {})) {
    const ref = `${node}.inputs.${port}`
    const list: Source[] = []
    for (const e of def.edges) if (e.to === ref) list.push({ kind: 'edge', from: e.from })
    for (const b of def.graph_input_bindings ?? []) if (b.to === ref) list.push({ kind: 'graph', from: b.from })
    for (const f of activeFields(desc?.config ?? [], n?.config ?? {})) {
      if (f.key && f.key in (n?.config ?? {}) && f.excludes?.some((r) => r.input === port)) list.push({ kind: 'literal', key: f.key })
    }
    out[port] = list.length > 0 ? list : [{ kind: 'unbound' }]
  }
  return out
}

// ---- operations ------------------------------------------------------------------

/** A committed field value: undefined removes the key, anything else is written as typed. */
export function configOp(node: string, key: string, value: JsonValue | undefined): Op {
  return value === undefined ? { op: 'unset_config', node, path: [key] } : { op: 'set_config', node, path: [key], value }
}

/** A committed loop Gate config value, as configOp: one targeted operation, so the rest of the loop stays as written. */
export function loopConfigOp(node: string, key: string, value: JsonValue | undefined): Op {
  return value === undefined ? { op: 'unset_loop_config', node, path: [key] } : { op: 'set_loop_config', node, path: [key], value }
}

/** The loop's attempt budget; undefined returns it to the type's default. */
export function loopAttemptsOp(node: string, value: number | undefined): Op {
  return value === undefined ? { op: 'set_loop_attempts', node } : { op: 'set_loop_attempts', node, max_attempts: value }
}

/** An integer typed as text: the number, '' for empty, or null when the text is not a plain decimal integer (`abc`, `1.5`, `0x10`, `Infinity`). */
export function parseInteger(text: string): number | '' | null {
  const s = text.trim()
  if (s === '') return ''
  if (!/^-?\d+$/.test(s)) return null
  const n = Number(s)
  return Number.isSafeInteger(n) ? n : null
}

/** Whether a map row other than except already has key (a commit would silently replace that entry). */
export const keyTaken = (rows: [string, unknown][], key: string, except: number): boolean => rows.some(([k], j) => j !== except && k === key)

/** Diagnostics under prefix that none of the shown lists holds, for a section to show at its own level. */
export function unshown(diags: Diagnostic[], prefix: string, shown: Diagnostic[][]): Diagnostic[] {
  const seen = new Set(shown.flat())
  return diagnosticsAt(diags, prefix).filter((d) => !seen.has(d))
}

// ---- loop starter ------------------------------------------------------------------

export type Starter = { kind: 'op'; op: Op } | { kind: 'pick'; outputs: string[] } | { kind: 'no-outputs' }

/**
 * The starter loop of requirement 11: the type's default attempts, a
 * json_predicate Gate with one declared input bound from a candidate output.
 * The candidate is the producer's first output whose type the Gate's input
 * descriptor accepts; without one the person picks the output first.
 */
export function loopStarter(node: string, n: NodeDef, desc: NodeTypeDescriptor, candidate?: string): Starter {
  const loop = desc.loop
  if (!loop) return { kind: 'no-outputs' }
  const outputs = Object.entries(n.outputs ?? {})
  if (outputs.length === 0) return { kind: 'no-outputs' }
  const kindField = loop.gate.find((f) => f.key === 'kind')
  const kind = kindField?.enum?.includes('json_predicate') ? 'json_predicate' : (kindField?.enum?.[0] ?? 'json_predicate')
  const active = activeFields(loop.gate, { kind })
  const inputField = active.find((f) => f.kind === 'input_name' && f.key === 'input')
  const accepted = inputField?.input_types ?? []
  const port = candidate ?? outputs.find(([, p]) => accepted.includes(p.type))?.[0]
  if (!port) return { kind: 'pick', outputs: outputs.map(([name]) => name) }
  const portType = n.outputs![port]!.type
  const operator = active.find((f) => f.key === 'operator')?.enum?.[0] ?? 'eq'
  const maxDefault = loop.fields.find((f) => f.key === 'max_attempts')?.default
  const op: Op = {
    op: 'set_loop',
    node,
    gate: { inputs: { candidate: { type: portType, required: true } }, config: { check_id: `${node}-check`, kind, input: 'candidate', field: ['ok'], operator, expected: true } },
    bindings: [{ from: `candidate.outputs.${port}`, to: 'gate.inputs.candidate' }],
  }
  if (typeof maxDefault === 'number') op.max_attempts = maxDefault
  return { kind: 'op', op }
}

/**
 * The spellings a loop binding can use: sources are the producer's candidate
 * outputs and its own inputs, targets the Gate's declared inputs. Which pairs
 * are allowed (equal types, unique targets) is the engine's answer to a
 * previewed set_loop_bindings, as can_connect is for edges; nothing here decides it.
 */
export function bindingEnds(n: NodeDef): { sources: string[]; targets: string[] } {
  return {
    sources: [...Object.keys(n.outputs ?? {}).map((p) => `candidate.outputs.${p}`), ...Object.keys(n.inputs ?? {}).map((p) => `node.inputs.${p}`)],
    targets: Object.keys(n.loop?.gate.inputs ?? {}).map((p) => `gate.inputs.${p}`),
  }
}

// ---- change preview ----------------------------------------------------------------

export interface Change {
  what: 'edge' | 'binding' | 'output' | 'loop_binding' | 'config' | 'port'
  how: 'removed' | 'added'
  text: string
}

const edgeKey = (e: EdgeDef) => `${e.from} → ${e.to}`

function diffList(what: Change['what'], before: string[], after: string[], out: Change[]) {
  const a = new Set(after)
  const b = new Set(before)
  for (const x of before) if (!a.has(x)) out.push({ what, how: 'removed', text: x })
  for (const x of after) if (!b.has(x)) out.push({ what, how: 'added', text: x })
}

/**
 * What an operation changed, from the definitions before and after the
 * engine applied it (a preview: the result is not adopted). Lists edges,
 * graph input bindings, graph outputs, loop bindings, ports and config values.
 */
export function describeChange(before: Definition, after: Definition): Change[] {
  const out: Change[] = []
  diffList('edge', before.edges.map(edgeKey), after.edges.map(edgeKey), out)
  diffList('binding', (before.graph_input_bindings ?? []).map(edgeKey), (after.graph_input_bindings ?? []).map(edgeKey), out)
  const outputs = (d: Definition) => Object.entries(d.outputs).map(([k, v]) => `${k} ← ${v.from}`)
  diffList('output', outputs(before), outputs(after), out)
  for (const id of Object.keys(before.nodes).sort()) {
    const a = before.nodes[id]!
    const b = after.nodes[id]
    if (!b) continue
    diffList('loop_binding', (a.loop?.bindings ?? []).map((x) => `${id}: ${edgeKey(x)}`), (b.loop?.bindings ?? []).map((x) => `${id}: ${edgeKey(x)}`), out)
    const ports = (n: NodeDef) => [...Object.entries(n.inputs ?? {}).map(([p, d]) => `${id}.inputs.${p}: ${d.type}${d.required ? ' (required)' : ''}`), ...Object.entries(n.outputs ?? {}).map(([p, d]) => `${id}.outputs.${p}: ${d.type}${d.required ? ' (required)' : ''}`), ...Object.entries(n.loop?.gate.inputs ?? {}).map(([p, d]) => `${id}.loop.gate.inputs.${p}: ${d.type}`)]
    diffList('port', ports(a), ports(b), out)
    const values = (n: NodeDef, prefix: string, cfg?: Record<string, unknown>) => Object.entries(cfg ?? {}).map(([k, v]) => `${id}.${prefix}${k} = ${JSON.stringify(v)}`)
    diffList('config', [...values(a, 'config.', a.config), ...values(a, 'loop.gate.config.', a.loop?.gate.config)], [...values(b, 'config.', b.config), ...values(b, 'loop.gate.config.', b.loop?.gate.config)], out)
  }
  return out
}

/** Value types a port may have (graph schema v0). */
export const PORT_TYPES = ['string', 'number', 'boolean', 'json', 'artifact_ref'] as const

/** Parses the text of an Advanced or scalar value: JSON when it is JSON, otherwise the text itself. */
export function parseLoose(text: string): JsonValue {
  try {
    return JSON.parse(text) as JsonValue
  } catch {
    return text
  }
}
