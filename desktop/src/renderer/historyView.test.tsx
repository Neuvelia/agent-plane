import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { HistoryListPage } from '../preload/api'
import { HistoryRows } from './historyView'

describe('history rows', () => {
  it('shows durable storage state as data and escapes graph names', () => {
    const page: HistoryListPage = { generation: 2, runs: [{
      run_id: 'run-1', project_id: 'project-1', graph_id: '<script>alert(1)</script>',
      started_at: '2026-11-08T10:00:00Z', storage_state: 'interrupted', pinned: false, revision: 1,
      completeness: [1, true, true], completeness_digest: 'sha256:fixture'
    }] }
    const html = renderToStaticMarkup(<HistoryRows page={page} loading={false} error={null} onOpen={() => {}} onMore={() => {}} />)
    expect(html).toContain('interrupted')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(html).not.toContain('<script>')
    expect(html).toContain('Inspect run run-1')
  })

  it('distinguishes empty, loading and unavailable history', () => {
    const render = (loading: boolean, error: string | null) => renderToStaticMarkup(<HistoryRows page={null} loading={loading} error={error} onOpen={() => {}} onMore={() => {}} />)
    expect(render(false, null)).toContain('No local run history')
    expect(render(true, null)).toContain('Loading local history')
    expect(render(false, 'Database cannot be read')).toContain('History unavailable: Database cannot be read')
  })
})
