import type { ApplyResult } from './useGraphDocument'
import type { Op, QueryResult } from './types'

/** The engine refuses a batch with more operations than this (edit.MaxOps, ADR-0021); a test keeps the two equal. */
export const MAX_OPS_PER_BATCH = 500

export type TargetAnswer = { ok: true; targets: Record<string, QueryResult> } | { ok: false; message: string }

/**
 * Asks the engine can_connect for every candidate input, in batches the engine
 * accepts. The first refused batch fails the whole answer: a partial list would
 * look like inputs that cannot be connected.
 */
export async function queryConnectTargets(apply: (ops: Op[]) => Promise<ApplyResult>, from: string, inputs: string[]): Promise<TargetAnswer> {
  const targets: Record<string, QueryResult> = {}
  for (let start = 0; start < inputs.length; start += MAX_OPS_PER_BATCH) {
    const chunk = inputs.slice(start, start + MAX_OPS_PER_BATCH)
    const r = await apply(chunk.map((to): Op => ({ op: 'can_connect', from, to })))
    if (!r.ok) return { ok: false, message: r.message }
    for (const res of r.results) {
      const to = chunk[res.index]
      if (to && res.query) targets[to] = res.query
    }
  }
  return { ok: true, targets }
}
