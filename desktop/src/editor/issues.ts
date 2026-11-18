import type { Diagnostic } from '../types'
import type { Definition } from './types'

// The Issues panel's model. It orders, filters and locates the engine's
// diagnostics; it never decides what is an error or whether a graph may run
// beyond reading severity (F2.4). Diagnostic text is shown as received.

export type Severity = 'error' | 'warning'

export type Target =
  | { kind: 'node'; id: string }
  | { kind: 'port'; node: string; side: 'inputs' | 'outputs'; port: string }
  | { kind: 'edge'; index: number; from: string; to: string }
  | { kind: 'binding'; index: number; from: string; to: string }
  | { kind: 'graph' }

export interface Issue {
  /** Stable between refreshes of the same result. */
  key: string
  severity: Severity
  code: string
  message: string
  suggestion?: string
  line?: number
  /** Only a parse issue can carry one, and only when the engine located the node. */
  column?: number
  target: Target
  /** Machine-readable name of the element (empty for the graph). */
  element: string
}

const severityOf = (s: string): Severity => (s === 'error' ? 'error' : 'warning')

/**
 * Finds the element a diagnostic concerns in the current definition. An element
 * that no longer exists (a race with an edit) falls back to the graph, so focus
 * handling never has to cope with a dangling reference.
 */
export function locate(d: Diagnostic, def: Definition | undefined): { target: Target; element: string } {
  const loc = d.location ?? {}
  if (def) {
    if (loc.edge_index !== undefined) {
      const e = def.edges[loc.edge_index]
      if (e) return { target: { kind: 'edge', index: loc.edge_index, from: e.from, to: e.to }, element: `${e.from} -> ${e.to}` }
    }
    if (loc.graph_input_binding_index !== undefined) {
      const b = def.graph_input_bindings?.[loc.graph_input_binding_index]
      if (b) return { target: { kind: 'binding', index: loc.graph_input_binding_index, from: b.from, to: b.to }, element: `${b.from} -> ${b.to}` }
    }
    const m = /^nodes\.([^.]+)\.(inputs|outputs)\.([^.]+)/.exec(loc.path ?? '')
    if (m) {
      const node = def.nodes[m[1]!]
      const side = m[2] as 'inputs' | 'outputs'
      if (node?.[side]?.[m[3]!]) return { target: { kind: 'port', node: m[1]!, side, port: m[3]! }, element: `${m[1]}.${side}.${m[3]}` }
    }
    if (loc.node_id && def.nodes[loc.node_id]) return { target: { kind: 'node', id: loc.node_id }, element: loc.node_id }
  }
  return { target: { kind: 'graph' }, element: '' }
}

const rank = (s: Severity) => (s === 'error' ? 0 : 1)

/** Errors first, then warnings, then by element, code and message: stable and deterministic. */
export function toIssues(diags: Diagnostic[], def: Definition | undefined): Issue[] {
  const issues = diags.map((d, i): Issue => {
    const { target, element } = locate(d, def)
    return { key: `${i}:${d.code}:${element}`, severity: severityOf(d.severity), code: d.code, message: d.message, suggestion: d.suggestion, line: d.line, target, element }
  })
  return issues.sort((a, b) => rank(a.severity) - rank(b.severity) || a.element.localeCompare(b.element) || a.code.localeCompare(b.code) || a.message.localeCompare(b.message))
}

/** A text that does not parse has exactly one issue and no others (requirement 8). */
export function parseIssue(message: string, line?: number, column?: number): Issue {
  return { key: 'parse', severity: 'error', code: 'YAML', message, line, column, target: { kind: 'graph' }, element: '' }
}

/** A request to take the person to an element; `nonce` makes repeating it observable. */
export interface FocusReq {
  target: Target
  line?: number
  nonce: number
}

export type Filter = 'all' | 'error' | 'warning'

export const filterIssues = (issues: Issue[], f: Filter): Issue[] => (f === 'all' ? issues : issues.filter((i) => i.severity === f))

export function issueCounts(issues: Issue[]): { all: number; error: number; warning: number } {
  const error = issues.filter((i) => i.severity === 'error').length
  return { all: issues.length, error, warning: issues.length - error }
}

export interface Badge {
  errors: number
  warnings: number
}

export interface Badges {
  nodes: Record<string, Badge>
  /** Keyed `FROM -> TO` as the definition spells the edge. */
  edges: Record<string, Badge>
  /** Keyed `NODE.inputs.PORT` or `NODE.outputs.PORT`. */
  ports: Record<string, Badge>
}

/** Counts per element, derived from the issues of the last result and nothing else. */
export function badgesOf(diags: Diagnostic[], def: Definition | undefined): Badges {
  const out: Badges = { nodes: {}, edges: {}, ports: {} }
  const bump = (m: Record<string, Badge>, k: string, sev: Severity) => {
    const b = (m[k] ??= { errors: 0, warnings: 0 })
    if (sev === 'error') b.errors++
    else b.warnings++
  }
  for (const d of diags) {
    const sev = severityOf(d.severity)
    const { target } = locate(d, def)
    // A node carries every diagnostic that names it, whatever element they point at.
    const nodeId = d.location?.node_id ?? (target.kind === 'port' ? target.node : target.kind === 'node' ? target.id : undefined)
    if (nodeId && def?.nodes[nodeId]) bump(out.nodes, nodeId, sev)
    if (target.kind === 'edge') bump(out.edges, `${target.from} -> ${target.to}`, sev)
    if (target.kind === 'port') bump(out.ports, `${target.node}.${target.side}.${target.port}`, sev)
  }
  return out
}

/** The first error in display order, for "show the first error". */
export const firstError = (issues: Issue[]): Issue | undefined => issues.find((i) => i.severity === 'error')
