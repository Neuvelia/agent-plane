import type { PendingApproval, RunSummary } from './types'

// Pure selectors for the Dashboard over the two lists the API already serves.
// "Now" is always passed in, so the output is deterministic.

export const ATTENTION_PREVIEW = 5
export const OUTCOME_DAYS = 7
export const OUTCOME_MIN_SLOTS = 14

export type AttentionItem =
  | { kind: 'approval'; approval: PendingApproval; at: string }
  | { kind: 'run'; run: RunSummary; at: string }

const time = (iso: string): number => {
  const t = Date.parse(iso)
  return Number.isNaN(t) ? 0 : t
}

/**
 * What cannot move without a person: pending approvals, and runs that failed or
 * were interrupted. Newest first. An approval carries no timestamp of its own, so
 * it is dated by the run it belongs to; at equal times approvals come first.
 */
export function needsAttention(runs: RunSummary[], approvals: PendingApproval[]): AttentionItem[] {
  const startedAt = new Map(runs.map((r) => [r.run_id, r.started_at]))
  const items: AttentionItem[] = [
    ...approvals.map((a): AttentionItem => ({ kind: 'approval', approval: a, at: startedAt.get(a.run_id) ?? '' })),
    ...runs.filter((r) => r.status === 'failed' || r.status === 'interrupted').map((r): AttentionItem => ({ kind: 'run', run: r, at: r.started_at })),
  ]
  return items.sort((a, b) => time(b.at) - time(a.at) || (a.kind === b.kind ? 0 : a.kind === 'approval' ? -1 : 1))
}

export const activeRuns = (runs: RunSummary[]): RunSummary[] => runs.filter((r) => r.status === 'running')

export type OutcomeTone = 'p' | 'x' | 'b' | 'n'

export interface Outcome {
  run: RunSummary
  tone: OutcomeTone
}

const TONE: Record<string, OutcomeTone> = { succeeded: 'p', failed: 'x', running: 'b' }

/** One dot per run started in the last 7 days, oldest first. */
export function outcomes(runs: RunSummary[], now: Date): { dots: Outcome[]; slots: number } {
  const since = now.getTime() - OUTCOME_DAYS * 24 * 3600 * 1000
  const dots = runs
    .filter((r) => time(r.started_at) >= since && time(r.started_at) <= now.getTime())
    .sort((a, b) => time(a.started_at) - time(b.started_at))
    .map((run): Outcome => ({ run, tone: TONE[run.status] ?? 'n' }))
  return { dots, slots: Math.max(OUTCOME_MIN_SLOTS, dots.length) }
}
