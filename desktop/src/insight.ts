import { eventTimeMs as ms } from './eventTime'
import type { Locale } from './i18n'
import type { EventSummary, NodeStatus, RunView } from './reducer'
import { cellsOf, nodeSegments, type CellTone, type TimelineSegment } from './timelineSegments'
import type { GraphSummary } from './types'

// Pure functions over the reduced run view. They are computed from events, are
// labelled as such in the UI, and have no authority over run state. A value that
// cannot be known is `undefined` and shown as unknown, never as 0.

export interface Kpis {
  /** Run start to its end, or to the newest event while it is still going. Never extrapolated. */
  wallClockMs?: number
  /** Sum of the durations of nodes that finished. Unknown until one has finished. */
  nodeTimeMs?: number
  /** Node time minus wall clock: what running independent nodes in parallel saved. Never negative. */
  savedMs?: number
  /** Extra attempts: attempts numbered above 1, across all nodes. */
  retries: number
  /** Always unknown: events carry no usage. */
  tokens?: undefined
}

export function runKpis(run: RunView): Kpis {
  const start = ms(run.startedAt)
  const end = ms(run.finishedAt) ?? ms(run.lastEventAt)
  const wallClockMs = start !== undefined && end !== undefined && end >= start ? end - start : undefined

  const durations = Object.values(run.nodes).flatMap((n) => (n.durationMs === undefined ? [] : [n.durationMs]))
  const nodeTimeMs = durations.length ? durations.reduce((a, b) => a + b, 0) : undefined

  const savedMs = wallClockMs !== undefined && nodeTimeMs !== undefined ? Math.max(0, nodeTimeMs - wallClockMs) : undefined
  const retries = Object.values(run.nodes).reduce((sum, n) => sum + n.attempts.filter((a) => a.number > 1).length, 0)
  return { wallClockMs, nodeTimeMs, savedMs, retries }
}

export type AgentTime = { kind: 'unknown' } | { kind: 'notApplicable' } | { kind: 'value'; ms: number }

/**
 * Sum of the durations of finished nodes whose graph type is `agent` (F2.6 requirement 3).
 * Not applicable for a graph with no agent node; unknown without a graph or until one has finished.
 */
export function agentTime(run: RunView, graph: GraphSummary | null): AgentTime {
  if (!graph) return { kind: 'unknown' }
  const agents = graph.nodes.filter((n) => n.type === 'agent')
  if (agents.length === 0) return { kind: 'notApplicable' }
  const finished = agents.flatMap((a) => {
    const n = run.nodes[a.id]
    return n?.finishedAt !== undefined && n.durationMs !== undefined ? [n.durationMs] : []
  })
  return finished.length === 0 ? { kind: 'unknown' } : { kind: 'value', ms: finished.reduce((a, b) => a + b, 0) }
}

/** "820 ms", "48.3s", "1m 12s". Units are the same in every language. */
export function formatDuration(durationMs: number, locale: Locale): string {
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`
  const seconds = durationMs / 1000
  if (seconds < 60) return `${new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(seconds)}s`
  const whole = Math.floor(seconds)
  const m = Math.floor(whole / 60)
  const s = whole % 60
  return s === 0 ? `${m}m` : `${m}m ${s}s`
}

export const MAX_TIMELINE_COLUMNS = 600

export interface TimelineRow {
  name: string
  /** One flag per column: the node was running (started, not yet finished) in that bucket. */
  active: boolean[]
  /** One tone per column from the node's segments; a column the segments do not cover but the node spanned is "running". */
  cells: (CellTone | undefined)[]
  segments: TimelineSegment[]
  attempts: number
  status: NodeStatus
  startSeconds: number
  durationMs?: number
}

export interface Timeline {
  rows: TimelineRow[]
  /** Seconds one dot stands for: 1 unless the run is longer than the column cap. */
  bucketSeconds: number
  columns: number
}

/** One row per node that started, one dot per second (bucketed past the column cap). */
export function timeline(run: RunView, maxColumns = MAX_TIMELINE_COLUMNS): Timeline | undefined {
  const start = ms(run.startedAt)
  const end = ms(run.finishedAt) ?? ms(run.lastEventAt)
  if (start === undefined || end === undefined || end < start) return undefined
  const seconds = Math.max(1, Math.ceil((end - start) / 1000))
  const bucketSeconds = Math.max(1, Math.ceil(seconds / maxColumns))
  const columns = Math.ceil(seconds / bucketSeconds)
  const rows = run.order.flatMap((id) => {
    const n = run.nodes[id]
    const from = ms(n?.startedAt)
    if (!n || from === undefined) return []
    const to = ms(n.finishedAt) ?? end
    const active = Array.from({ length: columns }, (_, c) => {
      const bucketStart = start + c * bucketSeconds * 1000
      const bucketEnd = bucketStart + bucketSeconds * 1000
      return from < bucketEnd && to >= bucketStart
    })
    const segments = nodeSegments(run, n, start, end)
    const cells = cellsOf(segments, columns, bucketSeconds).map((tone, c) => tone ?? (active[c] ? ('running' as const) : undefined))
    return [{ name: id, active, cells, segments, attempts: n.attempts.length, status: n.status, startSeconds: (from - start) / 1000, durationMs: n.durationMs }]
  })
  return { rows, bucketSeconds, columns }
}

export type EventFilter = 'all' | 'failed' | 'retried' | 'human'

export function filterEvents(tail: EventSummary[], filter: EventFilter): EventSummary[] {
  return filter === 'all' ? tail : tail.filter((e) => e.kind === filter)
}
