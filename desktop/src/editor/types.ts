import type { Diagnostic } from '../types'

// Wire shapes of the graph document API (ADR-0021). The sidecar owns every graph
// rule; the editor only sends text and operations and renders what comes back.

export interface OpenResponse {
  text: string
  hash: string
  version?: string
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
