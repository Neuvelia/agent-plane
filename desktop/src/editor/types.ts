import type { Diagnostic } from '../types'

// Wire shapes of the graph document API (ADR-0021). The sidecar owns every graph
// rule; the editor only sends text and operations and renders what comes back.

export interface PortDef {
  type: string
  required?: boolean
}
export interface NodeDef {
  type: string
  inputs?: Record<string, PortDef>
  outputs?: Record<string, PortDef>
}
export interface EdgeDef {
  from: string
  to: string
}
/** The parsed graph as the engine reports it. Only what the canvas draws is typed. */
export interface Definition {
  id: string
  name?: string
  metadata?: Record<string, unknown>
  nodes: Record<string, NodeDef>
  edges: EdgeDef[]
  graph_input_bindings?: EdgeDef[]
  outputs: Record<string, { from: string }>
}

/** Operations of ADR-0020 (ops_version 1) the canvas sends. */
export type Op =
  | { op: 'add_node'; id: string; type: string }
  | { op: 'remove_node'; id: string }
  | { op: 'rename_node'; id: string; new_id: string }
  | { op: 'set_layout'; nodes: Record<string, { x: number; y: number }> }
  | { op: 'add_edge' | 'remove_edge' | 'can_connect'; from: string; to: string }

export interface QueryResult {
  ok: boolean
  code?: string
  message?: string
}

export interface OpResult {
  index: number
  op: string
  query?: QueryResult
}

export interface OpError {
  code: string
  message: string
  op_index: number
  node?: string
  edge?: string
  suggestion?: string
}

export interface OpenResponse {
  text: string
  hash: string
  version?: string
  definition?: Definition
  results?: OpResult[]
  diagnostics: Diagnostic[]
  parse_error?: string
  readonly_canvas: boolean
  readonly_reasons?: string[]
  line_ending: 'lf' | 'crlf'
  label: string
}

export interface ValidateResponse {
  status: 'valid' | 'invalid'
  diagnostics: Diagnostic[]
  parse_error?: string
  version?: string
  definition?: Definition
  readonly_canvas?: boolean
  readonly_reasons?: string[]
}

export type SaveOutcome =
  | { kind: 'saved'; hash: string }
  | { kind: 'conflict'; currentHash: string }
  | { kind: 'exists' }
  | { kind: 'gone' }
  | { kind: 'failed'; code: string; message: string }

export interface StatResponse {
  exists: boolean
  hash?: string
  size?: number
}
