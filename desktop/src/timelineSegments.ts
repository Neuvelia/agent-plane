import { eventTimeMs } from './eventTime'
import type { NodeView, RunView } from './reducer'

// Attempt-aware timeline segments (F2.6 requirement 6): for one node, the
// non-overlapping intervals it spent queued, in each attempt, waiting for a
// human, or otherwise running. Pure function of the node's recorded timestamps.

export type SegmentKind = 'queued' | 'attempt' | 'human' | 'running'

export interface TimelineSegment {
  kind: SegmentKind
  /** Offsets from the run start, in seconds (fractional: the exact event timestamps). */
  startSeconds: number
  endSeconds: number
  /** Attempt number and the outcome the engine recorded; undefined while the attempt is unfinished. */
  attempt?: number
  outcome?: string
  /** The segment ends at the newest event because no event ended it. */
  provisional: boolean
}

interface Piece {
  kind: SegmentKind
  from: number
  to: number
  attempt?: number
  outcome?: string
  provisional: boolean
}

/** Cuts every piece that starts before the previous one ended, so the result never overlaps. */
function withoutOverlap(pieces: Piece[]): Piece[] {
  const sorted = [...pieces].sort((a, b) => a.from - b.from || a.to - b.to)
  const kept: Piece[] = []
  let cursor = -Infinity
  for (const piece of sorted) {
    const from = Math.max(piece.from, cursor)
    // An instant (an attempt that took less than a millisecond) is still a fact and stays.
    if (piece.to < from) continue
    kept.push({ ...piece, from })
    cursor = piece.to
  }
  return kept
}

/** The parts of [from, to] that no taken piece covers, as running pieces. */
function gaps(from: number, to: number, taken: Piece[], provisional: boolean): Piece[] {
  const out: Piece[] = []
  let cursor = from
  for (const piece of taken) {
    if (piece.from > cursor) out.push({ kind: 'running', from: cursor, to: Math.min(piece.from, to), provisional: false })
    cursor = Math.max(cursor, piece.to)
  }
  if (cursor < to) out.push({ kind: 'running', from: cursor, to, provisional })
  return out
}

function pieces(n: NodeView, nodeStart: number, nodeEnd: number, provisional: boolean): Piece[] {
  const out: Piece[] = []
  for (const a of n.attempts) {
    const from = eventTimeMs(a.startedAt)
    if (from === undefined) continue
    const to = eventTimeMs(a.finishedAt) ?? nodeEnd
    out.push({ kind: 'attempt', from, to, attempt: a.number, outcome: a.outcome, provisional: a.finishedAt === undefined && provisional })
  }
  const asked = eventTimeMs(n.approvalRequestedAt)
  if (asked !== undefined) {
    const decided = eventTimeMs(n.approvalDecidedAt)
    out.push({ kind: 'human', from: asked, to: decided ?? nodeEnd, provisional: decided === undefined && provisional })
  }
  return out.filter((p) => p.from >= nodeStart)
}

/**
 * nodeSegments returns the node's segments in time order, as offsets from `runStartMs`.
 * `endMs` is the run's end or its newest event; an unfinished node is measured to it.
 */
export function nodeSegments(run: RunView, n: NodeView, runStartMs: number, endMs: number): TimelineSegment[] {
  const started = eventTimeMs(n.startedAt)
  if (started === undefined) return []
  const finished = eventTimeMs(n.finishedAt)
  const nodeEnd = Math.max(started, finished ?? endMs)
  const provisional = finished === undefined && (run.status === 'running' || run.status === 'interrupted')
  const ready = eventTimeMs(n.readyAt)
  const queued: Piece[] = ready !== undefined && ready < started ? [{ kind: 'queued', from: ready, to: started, provisional: false }] : []
  const taken = withoutOverlap(pieces(n, started, nodeEnd, provisional))
  const all = [...queued, ...taken, ...gaps(started, nodeEnd, taken, provisional)].sort((a, b) => a.from - b.from)
  return all.map((p) => ({
    kind: p.kind,
    startSeconds: Math.max(0, p.from - runStartMs) / 1000,
    endSeconds: Math.max(0, p.to - runStartMs) / 1000,
    attempt: p.attempt,
    outcome: p.outcome,
    provisional: p.provisional,
  }))
}

/** What a dot column shows. A retry attempt (number above 1) has its own tone. */
export type CellTone = 'queued' | 'running' | 'attempt' | 'retry' | 'human'

export function toneOf(s: TimelineSegment): CellTone {
  if (s.kind === 'attempt') return (s.attempt ?? 1) > 1 ? 'retry' : 'attempt'
  return s.kind
}

/** One tone per column: the segment overlapping the column's bucket the most, the earlier one on a tie. */
export function cellsOf(segments: TimelineSegment[], columns: number, bucketSeconds: number): (CellTone | undefined)[] {
  return Array.from({ length: columns }, (_, c) => {
    const from = c * bucketSeconds
    const to = from + bucketSeconds
    let best: TimelineSegment | undefined
    let bestOverlap = 0
    for (const s of segments) {
      const overlap = Math.min(s.endSeconds, to) - Math.max(s.startSeconds, from)
      if (overlap > bestOverlap) {
        best = s
        bestOverlap = overlap
      }
    }
    return best ? toneOf(best) : undefined
  })
}
