import type { ConnectionOption, Op, QueryResult } from './types'
import type { ApplyResult } from './useGraphDocument'

// The connection query (F2.3 requirement 2). One `list_connections` operation
// answers, with the validator's own rules, which counterpart ports a port can be
// joined to and why not. The renderer reads `ok` and the message; it never
// compares types or looks for a cycle.

export type ConnectionAsk = { from: string } | { to: string }

export type ConnectionAnswer = { ok: true; options: ConnectionOption[] } | { ok: false; message: string }

/** Asks the engine for every counterpart of one port. A refused query is an error, never an empty list. */
export async function listConnections(apply: (ops: Op[]) => Promise<ApplyResult>, ask: ConnectionAsk): Promise<ConnectionAnswer> {
  const r = await apply([{ op: 'list_connections', ...ask }])
  if (!r.ok) return { ok: false, message: r.message }
  const query = r.results[0]?.query
  if (!query?.ok) return { ok: false, message: query?.message ?? 'the engine gave no answer' }
  return { ok: true, options: query.options ?? [] }
}

/** The engine's verdict per counterpart port, in the shape the canvas keeps while a connection is dragged. */
export function verdictsOf(options: ConnectionOption[]): Record<string, QueryResult> {
  return Object.fromEntries(options.map((o) => [o.ref, { ok: o.ok, code: o.code, message: o.message }]))
}
