import { interval } from './eventTime'
import type { NodeView, RunView } from './reducer'
import type { GraphSummary } from './types'

// Basic critical path (F2.6 requirement 4): the longest chain of data-dependent
// nodes by duration, over nodes that started. Queue time is not part of it, so
// the wall clock can exceed the total. Iterative and linear in nodes plus edges.

export interface CriticalPath {
  /** Node IDs from the first to the last node of the path; every consecutive pair is a graph edge. */
  nodes: string[]
  totalMs: number
  /** A node on the path had not finished, so its weight ends at the newest event. */
  soFar: boolean
}

/** The duration a node contributes, or undefined when it never started or has no knowable duration. */
function weightOf(run: RunView, n: NodeView): { ms: number; soFar: boolean } | undefined {
  if (n.startedAt === undefined) return undefined
  if (n.durationMs !== undefined && n.finishedAt !== undefined) return { ms: n.durationMs, soFar: false }
  if (n.finishedAt !== undefined) return undefined
  const ms = interval(n.startedAt, run.lastEventAt)
  return ms === undefined ? undefined : { ms, soFar: true }
}

interface Candidate {
  best: number
  /** The chosen predecessor on the longest path to this node. */
  from?: string
}

export function criticalPath(run: RunView, graph: GraphSummary): CriticalPath | undefined {
  const weights = new Map<string, { ms: number; soFar: boolean }>()
  for (const g of graph.nodes) {
    const n = run.nodes[g.id]
    const w = n && weightOf(run, n)
    if (w) weights.set(g.id, w)
  }
  if (weights.size === 0) return undefined

  const successors = new Map<string, string[]>()
  const remaining = new Map<string, number>()
  for (const id of weights.keys()) remaining.set(id, 0)
  for (const e of graph.edges) {
    if (!weights.has(e.from) || !weights.has(e.to)) continue
    successors.set(e.from, [...(successors.get(e.from) ?? []), e.to])
    remaining.set(e.to, remaining.get(e.to)! + 1)
  }

  // Kahn order, with node IDs ascending among the ready ones so ties are deterministic.
  const ready = [...remaining].filter(([, count]) => count === 0).map(([id]) => id).sort()
  const best = new Map<string, Candidate>()
  for (const id of ready) best.set(id, { best: weights.get(id)!.ms })
  const order: string[] = []
  while (ready.length > 0) {
    const id = ready.shift()!
    order.push(id)
    const here = best.get(id)!
    for (const next of successors.get(id) ?? []) {
      relax(best, next, id, here.best + weights.get(next)!.ms)
      const left = remaining.get(next)! - 1
      remaining.set(next, left)
      if (left === 0) insertSorted(ready, next)
    }
  }
  return trace(order, best, weights)
}

/** Keep the longer path; on equal length keep the lower predecessor ID. */
function relax(best: Map<string, Candidate>, id: string, from: string, total: number): void {
  const current = best.get(id)
  if (!current || total > current.best || (total === current.best && from < (current.from ?? ''))) best.set(id, { best: total, from })
}

function insertSorted(ids: string[], id: string): void {
  let at = ids.findIndex((other) => other > id)
  if (at < 0) at = ids.length
  ids.splice(at, 0, id)
}

function trace(order: string[], best: Map<string, Candidate>, weights: Map<string, { ms: number; soFar: boolean }>): CriticalPath | undefined {
  let end: string | undefined
  for (const id of [...order].sort()) if (end === undefined || best.get(id)!.best > best.get(end)!.best) end = id
  if (end === undefined) return undefined
  const nodes: string[] = []
  for (let at: string | undefined = end; at !== undefined; at = best.get(at)!.from) nodes.unshift(at)
  return { nodes, totalMs: best.get(end)!.best, soFar: nodes.some((id) => weights.get(id)!.soFar) }
}
