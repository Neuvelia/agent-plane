import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import fixIssueError from './fixtures/graph-open-editor-v2-error.json'
import fixIssue from './fixtures/graph-open-editor-v2.json'
import emptyOpen from './fixtures/graph-open-empty.json'
import { fromOpen, runBlock } from '../src/editor/doc'
import { HeaderBar } from '../src/editor/HeaderBar'
import { ProblemsDrawer, StatusBar, issuesOf } from '../src/editor/IssuesPanel'
import { issueCounts } from '../src/editor/issues'
import type { OpenResponse } from '../src/editor/types'
import type { GraphDocument } from '../src/editor/useGraphDocument'
import { I18nProvider } from '../src/i18n/context'

// The graph editor v2 chrome (F2.5f): one header row whose primary action
// explains why it cannot run, and a status bar with counts that opens the drawer.
// Every fact shown comes from the engine's recorded answer.

const doc = (open: unknown) => fromOpen('g', open as OpenResponse)
const g = { busy: null, recent: [], undo: () => undefined, redo: () => undefined, save: async () => undefined, open: () => undefined, close: () => undefined, openRecent: () => undefined } as unknown as GraphDocument

function header(open: unknown, locale: 'en' | 'pl' = 'en') {
  const d = doc(open)
  const block = runBlock(d.analysis)
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <HeaderBar g={g} doc={d} view="visual" onView={() => undefined} block={block} errorCount={issueCounts(issuesOf(d.analysis)).error} onShowFirstError={() => undefined} onRun={() => undefined} onNew={() => undefined} />
    </I18nProvider>,
  )
}

describe('graph editor v2 header', () => {
  it('one primary action, enabled when the engine reports no error [F2.5f-AC1]', () => {
    const html = header(fixIssue)
    expect(html.match(/class="btn pr"/g)).toHaveLength(1)
    expect(html).toMatch(/<button[^>]*class="btn pr"[^>]*data-testid="editor-save-run"/)
    expect(html).not.toMatch(/data-testid="editor-save-run"[^>]*disabled/)
    expect(html).not.toContain('data-testid="run-blocked"')
  })

  it('names the blocking error count next to Save and run, which it disables [F2.5f-AC1]', () => {
    const html = header(fixIssueError)
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*aria-describedby="er-blk"[^>]*data-testid="editor-save-run"/)
    expect(html).toContain('1 error blocks run')
    expect(header(fixIssueError, 'pl')).toContain('1 błąd blokuje uruchomienie')
  })

  it('an empty graph says to add a step', () => {
    expect(header(emptyOpen)).toContain('Add a step to run')
  })

  it('the file name opens the graph menu', () => {
    expect(header(fixIssue)).toMatch(/<button[^>]*aria-haspopup="menu"[^>]*aria-expanded="false"[^>]*data-testid="graph-menu"[^>]*>.*fix-issue\.yaml/)
  })
})

describe('graph editor v2 status bar and drawer', () => {
  const bar = (open: unknown, expanded = false) =>
    renderToStaticMarkup(
      <I18nProvider locale="en">
        <StatusBar analysis={doc(open).analysis} open={expanded} onToggle={() => undefined} version={doc(open).analysis.version} empty={false} />
      </I18nProvider>,
    )

  it('always shows the counts, as a button that controls the drawer [F2.5f-AC3]', () => {
    const html = bar(fixIssueError)
    expect(html).toMatch(/aria-expanded="false" aria-controls="er-probs"/)
    expect(html).toContain('1 error')
    expect(html).toContain('2 warnings')
    expect(html).toMatch(/Version [0-9a-f]{8}/)
  })

  it('a closed drawer is inert, an open one is not', () => {
    const drawer = (open: boolean) =>
      renderToStaticMarkup(
        <I18nProvider locale="en">
          <ProblemsDrawer analysis={doc(fixIssueError).analysis} open={open} onFocus={() => undefined} onRetry={() => undefined} />
        </I18nProvider>,
      )
    expect(drawer(false)).toMatch(/<section id="er-probs"[^>]*inert=""/)
    expect(drawer(true)).not.toMatch(/<section id="er-probs"[^>]*inert/)
    // errors first, each a real button that names where it goes
    const rows = [...drawer(true).matchAll(/data-severity="(\w+)"/g)].map((m) => m[1])
    expect(rows).toEqual(['error', 'warning', 'warning'])
  })
})
