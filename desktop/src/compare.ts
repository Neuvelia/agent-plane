import type { Attempt, CheckView, NodeView, RunView } from './reducer'

// F2.8a Attempt Compare: a pure view over two attempts that the reducer already
// holds. Nothing here is recorded or decided by the UI. Unknown stays unknown:
// an attempt without node_evidence is "not observed", never "0 files", and the
// scope badge comes only from the engine's changed_files_scope check result, so
// the UI never evaluates allowed_paths itself.

/** Why an attempt has no changed-file list. */
export type NotObserved = 'shell' | 'failed' | 'running' | 'not_recorded'

export interface FileRow {
  path: string
  /** Listed here and not in the other attempt's observed list (both observed). */
  isNew: boolean
  /** Listed as out of scope by this attempt's changed_files_scope check. */
  outsideScope: boolean
}

export type ScopeView =
  | { kind: 'not_checked' }
  /** `paths` are the out-of-scope paths the check listed; `omitted` counts paths beyond its evidence limit. */
  | { kind: 'checked'; paths: string[]; omitted: number }

export interface CompareSide {
  attempt: Attempt
  /** The engine's outcome, `interrupted` for an unfinished attempt of a run that is no longer running, or undefined while running. */
  outcome?: string
  /** attempt_started to attempt_finished, from event timestamps. Undefined when either is missing. */
  attemptMs?: number
  check?: CheckView
  files?: FileRow[]
  notObserved?: NotObserved
  scope: ScopeView
  /** Number of the attempt whose check result started this one; 0 when that attempt is not in the stream. */
  feedbackFrom?: number
  accepted: boolean
}

export interface FeedbackView {
  /** Number of the previous attempt, or undefined when it is not in the stream. */
  fromNumber?: number
  previousCheckId: string
  /** The referenced check result; undefined when it is missing from the stream. */
  check?: CheckView
}

export interface CompareModel {
  from: CompareSide
  to: CompareSide
  /** Feedback that started the To attempt, when it had any. */
  feedback?: FeedbackView
}

export const canCompare = (n: NodeView): boolean => n.attempts.length >= 2

/** From = the first rejected attempt (else the first), To = the last (FR2). */
export function defaultPair(n: NodeView): { from: string; to: string } | undefined {
  if (!canCompare(n)) return undefined
  const from = n.attempts.find((a) => a.outcome === 'rejected') ?? n.attempts[0]!
  return { from: from.id, to: n.attempts[n.attempts.length - 1]!.id }
}

const ms = (iso: string | undefined): number | undefined => {
  if (!iso) return undefined
  const t = Date.parse(iso)
  return Number.isNaN(t) ? undefined : t
}

function scopeOf(check: CheckView | undefined): ScopeView {
  if (!check || check.kind !== 'changed_files_scope') return { kind: 'not_checked' }
  const items = check.evidence.filter((e) => e.kind === 'changed_files_scope')
  const paths = items.flatMap((e) => (e.path ? [e.path] : []))
  const omitted = items.reduce((sum, e) => sum + (e.omitted ?? 0), 0)
  return { kind: 'checked', paths, omitted }
}

function notObserved(a: Attempt, kind: string | undefined, runActive: boolean): NotObserved {
  if (kind === 'shell') return 'shell'
  if (a.outcome === 'execution_error' || a.outcome === 'cancelled') return 'failed'
  if (a.outcome === undefined && runActive) return 'running'
  return 'not_recorded'
}

function side(run: RunView, n: NodeView, a: Attempt, other: Attempt, kind: string | undefined): CompareSide {
  const runActive = run.status === 'running'
  const start = ms(a.startedAt)
  const end = ms(a.finishedAt)
  const scope = scopeOf(a.check)
  const outside = new Set(scope.kind === 'checked' ? scope.paths : [])
  const otherFiles = other.changedFiles ? new Set(other.changedFiles) : undefined
  const s: CompareSide = {
    attempt: a,
    outcome: a.outcome ?? (runActive ? undefined : 'interrupted'),
    attemptMs: start !== undefined && end !== undefined ? Math.max(0, end - start) : undefined,
    check: a.check,
    scope,
    accepted: a.outcome === 'accepted' && n.status === 'succeeded',
  }
  if (a.changedFiles) s.files = a.changedFiles.map((path) => ({ path, isNew: otherFiles !== undefined && !otherFiles.has(path), outsideScope: outside.has(path) }))
  else s.notObserved = notObserved(a, kind, runActive)
  if (a.previousAttemptId) s.feedbackFrom = n.attempts.find((x) => x.id === a.previousAttemptId)?.number ?? 0
  return s
}

/** The compare model for two attempts of a node, or undefined when either ID is unknown. */
export function compareModel(run: RunView, n: NodeView, fromId: string, toId: string, kind?: string): CompareModel | undefined {
  const a = n.attempts.find((x) => x.id === fromId)
  const b = n.attempts.find((x) => x.id === toId)
  if (!a || !b) return undefined
  const model: CompareModel = { from: side(run, n, a, b, kind), to: side(run, n, b, a, kind) }
  if (b.previousAttemptId) {
    const prev = n.attempts.find((x) => x.id === b.previousAttemptId)
    const check = prev?.check && prev.check.checkId === b.previousCheckId ? prev.check : undefined
    model.feedback = { fromNumber: prev?.number, previousCheckId: b.previousCheckId ?? '', check }
  }
  return model
}
