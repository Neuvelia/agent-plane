import type { RunSummary } from './types'

// "Show values from run" (F2.3 requirement 16): which stored runs may be offered
// for the open graph, which is chosen by default, and when values may be shown.
// Pure functions of the run list, the open document and the person's choices.

export interface PickerRun {
  runId: string
  startedAt: string
  status: string
  /** The run executed the same semantic version as the open document. A run with no recorded version is never "same". */
  sameVersion: boolean
}

/**
 * pickerRuns lists the runs of the graph with this ID, newest first. Runs the store
 * could not read (`error`) are left out: they have no events to show. A run is
 * `sameVersion` only when both versions are known and equal; a missing version on
 * either side counts as different, so a value is never attributed to a definition
 * nobody can vouch for (decision 3).
 */
export function pickerRuns(runs: readonly RunSummary[], graphId: string | undefined, documentVersion: string | undefined): PickerRun[] {
  if (!graphId) return []
  return runs
    .filter((r) => r.graph_id === graphId && !r.error)
    .map((r) => ({
      runId: r.run_id,
      startedAt: r.started_at,
      status: r.status,
      sameVersion: !!r.graph_version && !!documentVersion && r.graph_version === documentVersion,
    }))
    .sort(newestFirst)
}

function newestFirst(a: PickerRun, b: PickerRun): number {
  const at = Date.parse(a.startedAt)
  const bt = Date.parse(b.startedAt)
  if (Number.isFinite(at) && Number.isFinite(bt) && at !== bt) return bt - at
  // Unparseable or equal times: a total, deterministic order.
  return a.startedAt === b.startedAt ? (a.runId < b.runId ? 1 : -1) : a.startedAt < b.startedAt ? 1 : -1
}

/** The person's explicit choices for the open document. Discard it when the document closes. */
export interface RunChoice {
  /** An explicit pick; undefined follows the default. */
  runId?: string
  /** Different-version runs whose values the person agreed to see. */
  confirmed: readonly string[]
}

export const noChoice: RunChoice = { confirmed: [] }

export const choose = (choice: RunChoice, runId: string | undefined): RunChoice => ({ ...choice, runId })

export const confirmVersion = (choice: RunChoice, runId: string): RunChoice =>
  choice.confirmed.includes(runId) ? choice : { ...choice, confirmed: [...choice.confirmed, runId] }

/** The default pick: the newest run of the same version. Without one nothing is chosen for the person. */
export function defaultRun(runs: readonly PickerRun[]): PickerRun | undefined {
  return runs.find((r) => r.sameVersion)
}

export interface ResolvedRun {
  run?: PickerRun
  /** The run is of another version and not confirmed: its values must not be read or shown. */
  needsConfirmation: boolean
  /** The run is of another version and confirmed: its values carry the "different version" label. */
  differentVersion: boolean
}

/**
 * resolveRun applies the choice to the current list. A choice whose run left the
 * list (pruned) falls back to the default instead of showing nothing misleading.
 */
export function resolveRun(runs: readonly PickerRun[], choice: RunChoice): ResolvedRun {
  const picked = choice.runId === undefined ? undefined : runs.find((r) => r.runId === choice.runId)
  const run = picked ?? defaultRun(runs)
  if (!run) return { needsConfirmation: false, differentVersion: false }
  const confirmed = run.sameVersion || choice.confirmed.includes(run.runId)
  return { run, needsConfirmation: !confirmed, differentVersion: !run.sameVersion && confirmed }
}

/** The run whose events may be loaded: chosen, and either the same version or confirmed. */
export function readableRunId(resolved: ResolvedRun): string | undefined {
  return resolved.run && !resolved.needsConfirmation ? resolved.run.runId : undefined
}
