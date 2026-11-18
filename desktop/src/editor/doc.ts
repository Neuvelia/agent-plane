import type { Diagnostic } from '../types'
import type { Definition, NodeInfo, NodeTypeDescriptor, OpenResponse, ValidateResponse } from './types'

// The open document, as a pure state machine. The renderer holds the TEXT of the
// graph (the file is the truth); everything about its meaning (diagnostics,
// version, parse errors) comes from the sidecar. No graph rule lives here.

/** Undo steps kept (spec F2.1, requirement 17). */
export const MAX_HISTORY = 200
/** Typing within this window is one undo step. */
export const COALESCE_MS = 800

export interface Analysis {
  status: 'ready' | 'validating'
  diagnostics: Diagnostic[]
  parseError?: string
  parseLine?: number
  /** Present only where the engine could locate the failing node. */
  parseColumn?: number
  version?: string
  /** The last definition the engine could parse. Kept while the text does not parse. */
  definition?: Definition
  /** Why the canvas must not edit this file (anchors, aliases, merge keys). */
  readonlyReasons?: string[]
  /** The list describes an older text than the draft. */
  stale: boolean
  /** The last validation could not reach the engine. */
  unreachable?: boolean
  /** Field descriptors of the node types in the graph, from the sidecar (F2.2). */
  nodeTypes?: Record<string, NodeTypeDescriptor>
  /** Per node: Advanced entries and the nodes it runs independently of (F2.2). */
  nodeInfo?: Record<string, NodeInfo>
}

export type Conflict = { kind: 'changed'; currentHash: string } | { kind: 'gone' } | { kind: 'exists' }

export interface DocState {
  grant: string
  label: string
  /** The file does not exist yet: the first save creates it. */
  isNew: boolean
  /** The text of the file as last opened or saved. */
  savedText: string
  /** Hash of the file's bytes, null while the file does not exist. */
  savedHash: string | null
  draft: string
  /** Older drafts, oldest first. */
  past: string[]
  future: string[]
  lastEditAt: number
  analysis: Analysis
  conflict?: Conflict
}

export const isDirty = (s: DocState): boolean => s.draft !== s.savedText

function analysisOf(r: { diagnostics: Diagnostic[]; parse_error?: string; parse_line?: number; parse_column?: number; version?: string; definition?: Definition; readonly_reasons?: string[]; node_types?: Record<string, NodeTypeDescriptor>; node_info?: Record<string, NodeInfo> }, previous?: Analysis): Analysis {
  const keep = !r.definition && !!r.parse_error
  return {
    // Like the definition, the inspector data of the last good text stays while the text does not parse.
    nodeTypes: keep ? previous?.nodeTypes : r.node_types,
    nodeInfo: keep ? previous?.nodeInfo : r.node_info,
    status: 'ready',
    diagnostics: r.diagnostics,
    parseError: r.parse_error,
    parseLine: r.parse_line,
    parseColumn: r.parse_column,
    version: r.version,
    // While the text does not parse the canvas keeps the last good definition (read-only).
    definition: r.definition ?? (r.parse_error ? previous?.definition : undefined),
    readonlyReasons: r.readonly_reasons,
    stale: false,
  }
}

export function fromOpen(grant: string, r: OpenResponse): DocState {
  return { grant, label: r.label, isNew: false, savedText: r.text, savedHash: r.hash, draft: r.text, past: [], future: [], lastEditAt: 0, analysis: analysisOf(r) }
}

/** A graph that exists only in the editor until its first save. */
export function fromNew(grant: string, label: string, text: string): DocState {
  return {
    grant,
    label,
    isNew: true,
    savedText: '',
    savedHash: null,
    draft: text,
    past: [],
    future: [],
    lastEditAt: 0,
    analysis: { status: 'validating', diagnostics: [], stale: true },
  }
}

/** A change made by typing. Typing close together is one undo step. */
export function type(s: DocState, text: string, now: number): DocState {
  if (text === s.draft) return s
  const boundary = s.past.length === 0 || now - s.lastEditAt > COALESCE_MS
  const past = boundary ? [...s.past, s.draft].slice(-MAX_HISTORY) : s.past
  return { ...s, draft: text, past, future: [], lastEditAt: now, analysis: { ...s.analysis, stale: true } }
}

/**
 * A change made by an operation. It is always its own undo step, and the
 * engine's answer already describes the new text, so no validation is pending.
 */
export function edited(s: DocState, r: OpenResponse): DocState {
  if (r.text === s.draft) return s
  return { ...s, draft: r.text, past: [...s.past, s.draft].slice(-MAX_HISTORY), future: [], lastEditAt: 0, analysis: analysisOf(r) }
}

export function undo(s: DocState): DocState {
  const previous = s.past[s.past.length - 1]
  if (previous === undefined) return s
  return { ...s, draft: previous, past: s.past.slice(0, -1), future: [s.draft, ...s.future].slice(0, MAX_HISTORY), lastEditAt: 0, analysis: { ...s.analysis, stale: true } }
}

export function redo(s: DocState): DocState {
  const [next, ...rest] = s.future
  if (next === undefined) return s
  return { ...s, draft: next, past: [...s.past, s.draft].slice(-MAX_HISTORY), future: rest, lastEditAt: 0, analysis: { ...s.analysis, stale: true } }
}

/** The text that was sent is now the file. Anything typed since stays a draft. */
export function saved(s: DocState, text: string, hash: string): DocState {
  return { ...s, isNew: false, savedText: text, savedHash: hash, conflict: undefined }
}

/** The file was read again (Reload, or a clean draft after an outside change): the draft is replaced. */
export function reloaded(s: DocState, r: OpenResponse): DocState {
  return { ...fromOpen(s.grant, r), label: r.label }
}

export function withConflict(s: DocState, conflict: Conflict | undefined): DocState {
  return { ...s, conflict }
}

export function validating(s: DocState): DocState {
  return { ...s, analysis: { ...s.analysis, status: 'validating' } }
}

/** A validation result counts only if it describes the text that is on screen now. */
export function analyzed(s: DocState, forText: string, r: ValidateResponse): DocState {
  if (forText !== s.draft) return s
  return { ...s, analysis: analysisOf(r, s.analysis) }
}

/**
 * Why "Save and run" is unavailable, or null when it may run. The engine's
 * diagnostics decide; this only reads them (F2.1 requirement 20).
 */
export type RunBlock = 'parse' | 'unreachable' | 'checking' | 'errors' | 'empty'
export function runBlock(a: Analysis): RunBlock | null {
  if (a.parseError) return 'parse'
  if (a.unreachable) return 'unreachable'
  if (a.stale || a.status === 'validating') return 'checking'
  if (a.diagnostics.some((d) => d.severity === 'error')) return 'errors'
  if (!a.definition || Object.keys(a.definition.nodes).length === 0) return 'empty'
  return null
}

/**
 * The engine could not be reached for this text. The previous list stays,
 * marked out of date, and nothing may run until a validation succeeds.
 */
export function validationFailed(s: DocState, forText: string): DocState {
  if (forText !== s.draft) return s
  return { ...s, analysis: { ...s.analysis, status: 'ready', stale: true, unreachable: true } }
}

/** Counts for the status line. */
export function counts(a: Analysis): { errors: number; warnings: number } {
  let errors = 0
  let warnings = 0
  for (const d of a.diagnostics) {
    if (d.severity === 'error') errors++
    else if (d.severity === 'warning') warnings++
  }
  return { errors, warnings }
}

/**
 * The graph id a new file gets from its name: it always satisfies the identifier
 * grammar (a lowercase letter, then lowercase letters, digits, hyphens or
 * underscores). Blank graphs and template drafts (F2.12) derive the same id.
 */
export function graphIdFromLabel(label: string): string {
  const base = label.replace(/\.ya?ml$/i, '').toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^[^a-z]+/, '').replace(/-+$/, '')
  return base || 'graph'
}

/** The starting text of a new graph: valid schema v0 with empty collections. */
export function newGraphText(label: string): string {
  return `schema_version: 0\nid: ${graphIdFromLabel(label)}\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n`
}
