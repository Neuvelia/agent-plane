import type { RunSummary } from './types'

// F2.9: pure helpers for the run list. Filtering and grouping only select and
// order what the store already reports; they never derive run state.

export type RunGroupBy = 'none' | 'repo' | 'graph'
export interface RunFilter {
  /** Empty string means every repository; NO_REPO selects runs without one. */
  repo: string
  /** Empty string means every graph. */
  graph: string
}
export interface RunGroup {
  key: string
  runs: RunSummary[]
}

/**
 * The key for runs recorded outside a repository or before F2.9 stored one.
 * Non-empty so it never collides with the empty "all repositories" filter value;
 * a NUL cannot occur in a repository name.
 */
export const NO_REPO = '\u0000none'

export function repoOf(run: RunSummary): string {
  return run.repo_name ?? NO_REPO
}

/** Wall-clock duration of a finished run, or undefined while it has no finish time. */
export function runDurationMs(run: RunSummary): number | undefined {
  if (!run.finished_at) return undefined
  const ms = Date.parse(run.finished_at) - Date.parse(run.started_at)
  return Number.isFinite(ms) && ms >= 0 ? ms : undefined
}

/** "main@1a2b3c4d", "@1a2b3c4d" for a detached HEAD, "main" without a commit, or undefined. */
export function sourceLabel(run: RunSummary): string | undefined {
  const commit = run.repo_commit?.slice(0, 8)
  if (run.repo_branch && commit) return `${run.repo_branch}@${commit}`
  if (commit) return `@${commit}`
  return run.repo_branch || undefined
}

/** Short form of the semantic graph version ("sha256-v0:abcdef…" keeps its prefix). */
export function shortVersion(version: string | undefined): string | undefined {
  if (!version) return undefined
  const i = version.lastIndexOf(':')
  const head = version.slice(0, i + 1)
  const hash = version.slice(i + 1)
  return hash.length > 10 ? `${head}${hash.slice(0, 10)}` : version
}

export function sortedUnique(values: string[]): string[] {
  return [...new Set(values)].sort((a, b) => a.localeCompare(b))
}

export function filterRuns(runs: RunSummary[], filter: RunFilter): RunSummary[] {
  return runs.filter((r) => (filter.repo === '' || repoOf(r) === filter.repo) && (filter.graph === '' || r.graph_id === filter.graph))
}

/** Groups keep the order of `runs` inside a group and order groups by their first run. */
export function groupRuns(runs: RunSummary[], by: RunGroupBy): RunGroup[] {
  if (by === 'none') return [{ key: '', runs }]
  const groups = new Map<string, RunSummary[]>()
  for (const r of runs) {
    const key = by === 'repo' ? repoOf(r) : r.graph_id
    const list = groups.get(key)
    if (list) list.push(r)
    else groups.set(key, [r])
  }
  return [...groups].map(([key, list]) => ({ key, runs: list }))
}
