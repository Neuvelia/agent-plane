import { nodeView, type CheckView, type RunView } from './reducer'

// F2.10 approval context: a pure view over the events the reducer already holds
// for the nodes a human gate depends on. It records and decides nothing. An
// unobserved changed-file list stays unknown, never "0 files" (see compare.ts).

/** Most changed files listed per upstream node; the rest is counted. */
export const MAX_CONTEXT_FILES = 10

export interface UpstreamContext {
  nodeId: string
  /** The check result of the node's last attempt, or of the node as a standalone Gate. */
  check?: CheckView
  /** Sorted Git-observed paths of that same last attempt, bounded. Undefined: not observed. */
  files?: string[]
  /** Observed paths beyond MAX_CONTEXT_FILES. */
  moreFiles: number
}

/**
 * Context for each direct dependency of a human node, in the graph's edge order without duplicates.
 * The check and the changed files come from the same attempt, the node's last one: an earlier,
 * rejected attempt never lends its files or its check to the attempt being approved.
 */
export function approvalContext(run: RunView, upstream: readonly string[]): UpstreamContext[] {
  return [...new Set(upstream)].map((id) => {
    const n = nodeView(run, id)
    const last = n.attempts[n.attempts.length - 1]
    const all = last?.changedFiles && [...last.changedFiles].sort()
    return {
      nodeId: id,
      check: last ? last.check : n.check,
      files: all?.slice(0, MAX_CONTEXT_FILES),
      moreFiles: Math.max(0, (all?.length ?? 0) - MAX_CONTEXT_FILES),
    }
  })
}
