import { interval } from './eventTime'
import type { NodeView, RunView } from './reducer'

// Where a run waited (F2.6 requirement 2): queued by the scheduler, or waiting
// for a human. Pure functions of the reduced run; a wait that cannot be known is
// undefined, never 0, and one with no end yet is measured to the newest event
// (never to the clock) and marked `soFar`.

/** A figure in ms; `soFar` means it ends at the newest event because no event ended it. */
export interface Measured {
  ms: number
  soFar: boolean
}

/** The run can still produce events, or stopped without a terminal one. */
const unfinished = (run: RunView): boolean => run.status === 'running' || run.status === 'interrupted'

const fixed = (ms: number | undefined): Measured | undefined => (ms === undefined ? undefined : { ms, soFar: false })
const open = (ms: number | undefined): Measured | undefined => (ms === undefined ? undefined : { ms, soFar: true })

/** node_ready to node_started. A node that is ready and has not started yet is measured to the newest event. */
export function queuedWait(run: RunView, n: NodeView): Measured | undefined {
  if (n.readyAt === undefined) return undefined
  if (n.startedAt !== undefined) return fixed(interval(n.readyAt, n.startedAt))
  const waiting = unfinished(run) && (n.status === 'ready' || n.status === 'pending')
  return waiting ? open(interval(n.readyAt, run.lastEventAt)) : undefined
}

/** approval_requested to approval_decided; a request nobody decided yet is measured to the newest event. */
export function humanWait(run: RunView, n: NodeView): Measured | undefined {
  if (n.approvalRequestedAt === undefined) return undefined
  if (n.approvalDecidedAt !== undefined) return fixed(interval(n.approvalRequestedAt, n.approvalDecidedAt))
  const waiting = unfinished(run) && (n.status === 'waiting_human' || n.status === 'interrupted')
  return waiting ? open(interval(n.approvalRequestedAt, run.lastEventAt)) : undefined
}

export type WaitKind = 'queue' | 'human'

export interface LongestWait {
  nodeId: string
  kind: WaitKind
  ms: number
  soFar: boolean
}

export interface WaitSummary {
  /** Total queued time of the nodes whose wait is known; undefined when none is. */
  queuedMs?: number
  /** Total human wait; undefined when no node asked for approval. */
  humanWaitMs?: number
  /** Some figure above ends at the newest event rather than at a recorded event. */
  soFar: boolean
  queueSoFar: boolean
  humanSoFar: boolean
  /** Nodes whose wait could not be determined (a missing timestamp); the totals skip them. */
  unknownNodes: number
  /** At most three nodes, longest wait first, ties by node ID. */
  longest: LongestWait[]
}

const MAX_LONGEST = 3

function longestOf(id: string, queue: Measured | undefined, human: Measured | undefined): LongestWait | undefined {
  const waits: LongestWait[] = []
  if (queue && queue.ms > 0) waits.push({ nodeId: id, kind: 'queue', ...queue })
  if (human && human.ms > 0) waits.push({ nodeId: id, kind: 'human', ...human })
  return waits.sort((a, b) => b.ms - a.ms)[0]
}

/** A node that started with no node_ready, or whose approval is only half recorded, cannot be measured. */
function unmeasurable(n: NodeView, queue: Measured | undefined): boolean {
  const queueUnknown = n.startedAt !== undefined && queue === undefined
  const humanUnknown = n.approvalDecidedAt !== undefined && n.approvalRequestedAt === undefined
  return queueUnknown || humanUnknown
}

export function waitSummary(run: RunView): WaitSummary {
  let queued: number | undefined
  let human: number | undefined
  let queueSoFar = false
  let humanSoFar = false
  let unknownNodes = 0
  const candidates: LongestWait[] = []
  for (const id of run.order) {
    const n = run.nodes[id]!
    const q = queuedWait(run, n)
    const h = humanWait(run, n)
    if (q) queued = (queued ?? 0) + q.ms
    if (h) human = (human ?? 0) + h.ms
    queueSoFar ||= !!q?.soFar
    humanSoFar ||= !!h?.soFar
    if (unmeasurable(n, q)) unknownNodes += 1
    const longest = longestOf(id, q, h)
    if (longest) candidates.push(longest)
  }
  candidates.sort((a, b) => b.ms - a.ms || (a.nodeId < b.nodeId ? -1 : 1))
  return { queuedMs: queued, humanWaitMs: human, soFar: queueSoFar || humanSoFar, queueSoFar, humanSoFar, unknownNodes, longest: candidates.slice(0, MAX_LONGEST) }
}
