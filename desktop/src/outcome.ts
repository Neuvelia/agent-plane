import type { NodeView, RunView } from './reducer'

// "What failed" at run level (F2.6 requirement 5): each failed node with the
// engine's own failure kind, diagnostic codes and the reason of its last red
// check, plus how many nodes were skipped or cancelled. Reasons are untrusted
// text, kept as received. Nothing here decides a status: it only reads facts.

export interface FailedNode {
  nodeId: string
  failureKind?: string
  diagnosticCodes: string[]
  /** Reason of the node's last red check, when it has one. */
  reason?: string
  /** A human node that rejected: the decision and the comment as recorded. */
  decision?: { decision: string; comment?: string }
}

export interface Outcome {
  failed: FailedNode[]
  skipped: number
  cancelled: number
  interrupted: number
  /** The run has not ended: the entries are the failures seen so far. */
  soFar: boolean
}

function lastRedReason(n: NodeView): string | undefined {
  for (let i = n.attempts.length - 1; i >= 0; i--) {
    const check = n.attempts[i]!.check
    if (check?.verdict === 'red') return check.reason
  }
  return n.check?.verdict === 'red' ? n.check.reason : undefined
}

function failedNode(id: string, n: NodeView): FailedNode {
  const rejected = n.approval?.decision === 'rejected'
  return {
    nodeId: id,
    failureKind: n.failureKind,
    diagnosticCodes: n.diagnosticCodes,
    reason: lastRedReason(n),
    decision: rejected ? { decision: 'rejected', comment: n.approval?.comment } : undefined,
  }
}

/**
 * outcomeSummary is undefined for a run that succeeded, and for a running run
 * with no failed node: those have nothing to explain.
 */
export function outcomeSummary(run: RunView): Outcome | undefined {
  const nodes = run.order.map((id) => [id, run.nodes[id]!] as const)
  const failed = nodes.filter(([, n]) => n.status === 'failed').map(([id, n]) => failedNode(id, n))
  const count = (status: NodeView['status']) => nodes.filter(([, n]) => n.status === status).length
  const ended = run.status === 'failed' || run.status === 'cancelled' || run.status === 'interrupted'
  if (!ended && failed.length === 0) return undefined
  if (run.status === 'succeeded') return undefined
  return { failed, skipped: count('skipped'), cancelled: count('cancelled'), interrupted: count('interrupted'), soFar: run.status === 'running' }
}
