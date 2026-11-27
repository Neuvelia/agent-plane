import { displayStatus, nodeView, type Attempt, type NodeStatus, type NodeView, type RunView } from './reducer'
import type { GraphSummary } from './types'

// Pure selectors for the Inbox detail pane (F2.5b) over one reduced run. They
// read only what the events recorded: nothing is guessed or timed here.

/** Lines of failure output the detail pane shows; the full log stays in the run inspector. */
export const FAILURE_LINES = 24

const ACTIVE: NodeStatus[] = ['running', 'retrying', 'waiting_human']

/** The node a stopped run stopped at: the first failed node, else the first interrupted one. */
export function stopNode(run: RunView): NodeView | undefined {
  const nodes = run.order.map((id) => nodeView(run, id))
  return nodes.find((n) => n.status === 'failed') ?? nodes.find((n) => n.status === 'interrupted')
}

/** The first node that is working or waiting for a person, with its display status. */
export function activeNode(run: RunView): { node: NodeView; status: NodeStatus } | undefined {
  for (const id of run.order) {
    const node = nodeView(run, id)
    const status = displayStatus(node)
    if (ACTIVE.includes(status)) return { node, status }
  }
  return undefined
}

export const lastAttempt = (n: NodeView): Attempt | undefined => n.attempts[n.attempts.length - 1]

/** Node IDs whose output was accepted, in the order the run reached them. */
export function acceptedNodes(run: RunView): string[] {
  return run.order.filter((id) => nodeView(run, id).status === 'succeeded')
}

function tail(text: string, lines: number): string {
  const all = text.replace(/\s+$/, '').split('\n')
  return all.slice(Math.max(0, all.length - lines)).join('\n')
}

/**
 * What the person reads first about a failure: the newest check's reason and
 * its output, else the node's own log. Output is cut to the last FAILURE_LINES lines.
 */
export function failureText(n: NodeView): { reason?: string; output: string } {
  const check = [...n.attempts].reverse().find((a) => a.check)?.check ?? n.check
  const evidence = check?.evidence.find((e) => e.stderr || e.stdout)
  const output = evidence ? (evidence.stderr || evidence.stdout || '') : n.log
  return { reason: check?.reason || undefined, output: tail(output, FAILURE_LINES) }
}

export interface NodeRow {
  id: string
  status: NodeStatus
  /** Recorded duration, or time since the node started up to the newest event while it runs. */
  durationMs?: number
  attempt?: { number: number; max: number }
  /** Upstream nodes not yet accepted, for a node that has not started. */
  waitsFor: string[]
}

function elapsed(n: NodeView, run: RunView): number | undefined {
  if (n.durationMs !== undefined) return n.durationMs
  if (!n.startedAt || !run.lastEventAt) return undefined
  const ms = Date.parse(run.lastEventAt) - Date.parse(n.startedAt)
  return Number.isNaN(ms) ? undefined : Math.max(0, ms)
}

/** One row per graph node, in graph order: status, time, attempt and what it waits for. */
export function nodeRows(run: RunView, graph: GraphSummary): NodeRow[] {
  return graph.nodes.map(({ id }) => {
    const n = nodeView(run, id)
    const status = displayStatus(n)
    const last = lastAttempt(n)
    const started = n.startedAt !== undefined
    const waitsFor = started ? [] : graph.edges.filter((e) => e.to === id && nodeView(run, e.from).status !== 'succeeded').map((e) => e.from)
    return { id, status, durationMs: started ? elapsed(n, run) : undefined, attempt: last && last.max > 1 ? { number: last.number, max: last.max } : undefined, waitsFor: [...new Set(waitsFor)] }
  })
}
