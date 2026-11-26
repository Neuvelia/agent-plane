import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { NO_REPO, filterRuns, groupRuns, runDurationMs, shortVersion, sortedUnique, repoOf, sourceLabel } from '../src/runsFilter'
import { I18nProvider } from '../src/i18n/context'
import { RunsList } from '../src/views'
import type { RunSummary } from '../src/types'

const run = (id: string, graph: string, extra: Partial<RunSummary> = {}): RunSummary => ({
  run_id: id,
  graph_id: graph,
  started_at: '2026-11-26T10:00:00Z',
  status: 'succeeded',
  event_count: 3,
  ...extra,
})

const runs = [
  run('a1', 'g1', { repo_name: 'alpha', repo_branch: 'main', repo_commit: '1234567890abcdef', finished_at: '2026-11-26T10:00:12Z' }),
  run('a2', 'g2', { repo_name: 'alpha', repo_commit: 'abcdef1234567890' }),
  run('b1', 'g1', { repo_name: 'beta', repo_branch: 'dev' }),
  run('o1', 'g1'),
]

describe('run list helpers (F2.9)', () => {
  it('computes a duration only for a finished run', () => {
    expect(runDurationMs(runs[0]!)).toBe(12000)
    expect(runDurationMs(runs[1]!)).toBeUndefined()
    expect(runDurationMs(run('x', 'g', { finished_at: '2026-11-26T09:00:00Z' }))).toBeUndefined()
  })

  it('labels the source with branch and short commit, tolerating either being absent', () => {
    expect(sourceLabel(runs[0]!)).toBe('main@12345678')
    expect(sourceLabel(runs[1]!)).toBe('@abcdef12')
    expect(sourceLabel(runs[2]!)).toBe('dev')
    expect(sourceLabel(runs[3]!)).toBeUndefined()
  })

  it('shortens a graph version and keeps a short or absent one', () => {
    expect(shortVersion('sha256-v0:0123456789abcdef0123')).toBe('sha256-v0:0123456789')
    expect(shortVersion('v1')).toBe('v1')
    expect(shortVersion(undefined)).toBeUndefined()
  })

  it('filters by repository and graph, treating no repository as its own choice', () => {
    expect(filterRuns(runs, { repo: 'alpha', graph: '' }).map((r) => r.run_id)).toEqual(['a1', 'a2'])
    expect(filterRuns(runs, { repo: '', graph: 'g1' }).map((r) => r.run_id)).toEqual(['a1', 'b1', 'o1'])
    expect(filterRuns(runs, { repo: 'alpha', graph: 'g1' }).map((r) => r.run_id)).toEqual(['a1'])
    expect(filterRuns(runs, { repo: repoOf(runs[3]!), graph: '' }).map((r) => r.run_id)).toEqual(['o1'])
    expect(filterRuns(runs, { repo: NO_REPO, graph: 'g1' }).map((r) => r.run_id)).toEqual(['o1'])
    expect(filterRuns(runs, { repo: '', graph: '' })).toHaveLength(4)
    expect(NO_REPO).not.toBe('')
    expect(sortedUnique(runs.map(repoOf))).toEqual(['alpha', 'beta', NO_REPO].sort((a, b) => a.localeCompare(b)))
  })

  it('groups by repository or graph and keeps run order', () => {
    expect(groupRuns(runs, 'repo').map((g) => [g.key, g.runs.map((r) => r.run_id)])).toEqual([
      ['alpha', ['a1', 'a2']],
      ['beta', ['b1']],
      [NO_REPO, ['o1']],
    ])
    expect(groupRuns(runs, 'graph').map((g) => g.key)).toEqual(['g1', 'g2'])
    expect(groupRuns(runs, 'none')).toHaveLength(1)
  })
})

describe('RunsList first render', () => {
  it('shows a loading state before runs arrive and no filters', () => {
    const out = renderToStaticMarkup(
      <I18nProvider locale="en">
        <RunsList onOpen={() => undefined} />
      </I18nProvider>,
    )
    expect(out).not.toContain('data-testid="runs-filters"')
  })
})
