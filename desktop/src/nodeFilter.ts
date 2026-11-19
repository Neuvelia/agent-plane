import type { RunView } from './reducer'
import type { GraphSummary } from './types'

// Node filters of the overview (F2.6 requirement 7): Failed, Retried, Human and
// Slow. View state only; the predicates read facts the engine recorded and the
// graph summary, and the event log keeps its own, separate filters.

export type NodeFilter = 'all' | 'failed' | 'retried' | 'human' | 'slow'

export const NODE_FILTERS: readonly NodeFilter[] = ['all', 'failed', 'retried', 'human', 'slow']

/** Slow needs a sample: at least this many finished nodes (decision 1). */
export const SLOW_MIN_FINISHED = 3

/** A node is never slow under this duration, however short the rest of the run was. */
export const SLOW_FLOOR_MS = 1000

const finishedDurations = (run: RunView): number[] =>
  Object.values(run.nodes).flatMap((n) => (n.durationMs !== undefined && n.finishedAt !== undefined ? [n.durationMs] : []))

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 1 ? sorted[mid]! : (sorted[mid - 1]! + sorted[mid]!) / 2
}

/** max(1 s, 2 x median) of the finished nodes' durations; undefined with fewer than three finished nodes. */
export function slowThresholdMs(run: RunView): number | undefined {
  const durations = finishedDurations(run)
  return durations.length < SLOW_MIN_FINISHED ? undefined : Math.max(SLOW_FLOOR_MS, 2 * median(durations))
}

/** Why a filter cannot be offered, or undefined when it can. */
export type FilterUnavailable = 'graph' | 'sample'

export function filterUnavailable(run: RunView, graph: GraphSummary | null, filter: NodeFilter): FilterUnavailable | undefined {
  if (filter === 'human') return graph ? undefined : 'graph'
  if (filter === 'slow') return slowThresholdMs(run) === undefined ? 'sample' : undefined
  return undefined
}

/** The IDs of the nodes a filter selects. A filter that cannot be offered selects nothing. */
export function matchingNodes(run: RunView, graph: GraphSummary | null, filter: NodeFilter): string[] {
  const ids = graph ? graph.nodes.map((n) => n.id) : run.order
  if (filter === 'all') return ids
  if (filterUnavailable(run, graph, filter)) return []
  const types = new Map((graph?.nodes ?? []).map((n) => [n.id, n.type]))
  const slow = slowThresholdMs(run)
  return ids.filter((id) => {
    const n = run.nodes[id]
    if (!n) return false
    if (filter === 'failed') return n.status === 'failed'
    if (filter === 'retried') return n.attempts.length > 1
    if (filter === 'human') return types.get(id) === 'human'
    return n.durationMs !== undefined && n.finishedAt !== undefined && slow !== undefined && n.durationMs >= slow
  })
}

/** How many nodes each filter selects, for the option labels. */
export function filterCounts(run: RunView, graph: GraphSummary | null): Record<NodeFilter, number> {
  const counts = {} as Record<NodeFilter, number>
  for (const f of NODE_FILTERS) counts[f] = matchingNodes(run, graph, f).length
  return counts
}
