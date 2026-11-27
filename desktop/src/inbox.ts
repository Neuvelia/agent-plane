import type { PendingApproval, RunSummary } from './types'

// Pure selectors for the Inbox (F2.5b) over the two lists the API already serves.
// "Now" is always passed in, so the output is deterministic.

/** What a queue row is about. `failed` and `interrupted` are run statuses; `approval` is a pending human gate. */
export type InboxKind = 'approval' | 'failed' | 'interrupted' | 'running'

/** Queue sections, in display order. */
export type InboxGroup = 'waiting' | 'stopped' | 'running'

export type InboxFilter = 'all' | 'approvals' | 'stopped' | 'running'

export interface InboxItem {
  /** Stable across polls: the approval's node run ID, or the run ID. */
  key: string
  kind: InboxKind
  group: InboxGroup
  runId: string
  graphId: string
  /** When the item started to exist, as far as the lists say: the run's start (an approval carries no timestamp of its own). */
  at: string
  approval?: PendingApproval
}

const GROUP_ORDER: InboxGroup[] = ['waiting', 'stopped', 'running']

const time = (iso: string): number => {
  const t = Date.parse(iso)
  return Number.isNaN(t) ? 0 : t
}

function runItem(run: RunSummary): InboxItem | null {
  if (run.status === 'failed' || run.status === 'interrupted') return { key: run.run_id, kind: run.status, group: 'stopped', runId: run.run_id, graphId: run.graph_id, at: run.started_at }
  if (run.status === 'running') return { key: run.run_id, kind: 'running', group: 'running', runId: run.run_id, graphId: run.graph_id, at: run.started_at }
  return null
}

/**
 * The triage queue: pending approvals ("waiting for you"), then failed or
 * interrupted runs ("stopped"), then running runs. Newest first inside a group.
 * A running run that waits for a listed approval appears once, as that approval.
 * An approval is dated by the run it belongs to; one whose run is not listed
 * keeps an empty date and sorts last in its group.
 */
export function inboxQueue(runs: RunSummary[], approvals: PendingApproval[]): InboxItem[] {
  const byId = new Map(runs.map((r) => [r.run_id, r]))
  const waiting = new Set(approvals.map((a) => a.run_id))
  const items: InboxItem[] = [
    ...approvals.map((a): InboxItem => ({ key: a.node_run_id, kind: 'approval', group: 'waiting', runId: a.run_id, graphId: byId.get(a.run_id)?.graph_id ?? a.run_id.slice(0, 8), at: byId.get(a.run_id)?.started_at ?? '', approval: a })),
    ...runs.map(runItem).filter((i): i is InboxItem => i !== null && !(i.kind === 'running' && waiting.has(i.runId))),
  ]
  return items.sort((a, b) => GROUP_ORDER.indexOf(a.group) - GROUP_ORDER.indexOf(b.group) || time(b.at) - time(a.at))
}

/**
 * The live queue plus approvals already decided on this screen that the list no longer
 * holds: each keeps its group, after the live items of that group.
 */
export function withDecided(live: InboxItem[], decided: InboxItem[]): InboxItem[] {
  const keys = new Set(live.map((i) => i.key))
  const gone = decided.filter((i) => !keys.has(i.key))
  return GROUP_ORDER.flatMap((g) => [...live.filter((i) => i.group === g), ...gone.filter((i) => i.group === g)])
}

const FILTER_GROUP: Record<Exclude<InboxFilter, 'all'>, InboxGroup> = { approvals: 'waiting', stopped: 'stopped', running: 'running' }

export function filterQueue(items: InboxItem[], filter: InboxFilter): InboxItem[] {
  return filter === 'all' ? items : items.filter((i) => i.group === FILTER_GROUP[filter])
}

export function queueCounts(items: InboxItem[]): Record<InboxFilter, number> {
  const count = (g: InboxGroup) => items.filter((i) => i.group === g).length
  return { all: items.length, approvals: count('waiting'), stopped: count('stopped'), running: count('running') }
}

/** The item to show: the one the person picked while it is still queued, else the first. */
export function selectItem(items: InboxItem[], picked: string | null): InboxItem | undefined {
  return items.find((i) => i.key === picked) ?? items[0]
}

/** The next item after a decision: the first queued item that is not the one just handled. */
export function nextItem(items: InboxItem[], current: string): InboxItem | undefined {
  return items.find((i) => i.key !== current)
}

export const RECENT_RUNS = 14

export type OutcomeTone = 'p' | 'x' | 'b' | 'n'

export interface Outcome {
  run: RunSummary
  tone: OutcomeTone
}

const TONE: Record<string, OutcomeTone> = { succeeded: 'p', failed: 'x', running: 'b' }

/** The newest RECENT_RUNS runs, oldest first, each with the tone of its status. */
export function recentOutcomes(runs: RunSummary[]): Outcome[] {
  return [...runs]
    .sort((a, b) => time(b.started_at) - time(a.started_at))
    .slice(0, RECENT_RUNS)
    .reverse()
    .map((run): Outcome => ({ run, tone: TONE[run.status] ?? 'n' }))
}

/** Milliseconds from `iso` to `now`, or undefined when the time is unknown. Never negative. */
export function ageMs(iso: string, now: Date): number | undefined {
  const t = Date.parse(iso)
  return Number.isNaN(t) ? undefined : Math.max(0, now.getTime() - t)
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/**
 * "14 min", "3 h", "2 d" in the active locale ("14m", "3h" with `narrow`, as in the
 * sidebar); undefined under a minute (the view says "just now").
 */
export function formatAge(ms: number, locale: string, display: 'short' | 'narrow' = 'short'): string | undefined {
  if (ms < MINUTE) return undefined
  const [unit, size] = ms < HOUR ? (['minute', MINUTE] as const) : ms < DAY ? (['hour', HOUR] as const) : (['day', DAY] as const)
  return new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: display }).format(Math.floor(ms / size))
}

export const SIDEBAR_RECENT = 5

/** The newest `count` runs, newest first (the sidebar's recent list). */
export function newestRuns(runs: RunSummary[], count = SIDEBAR_RECENT): RunSummary[] {
  return [...runs].sort((a, b) => time(b.started_at) - time(a.started_at)).slice(0, count)
}
