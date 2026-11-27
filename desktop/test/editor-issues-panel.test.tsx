import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Analysis } from '../src/editor/doc'
import { ProblemsDrawer } from '../src/editor/IssuesPanel'
import { I18nProvider } from '../src/i18n/context'

const parsed = (extra: Partial<Analysis>): Analysis => ({ status: 'ready', diagnostics: [], parseError: 'parse graph definition: yaml: broken', stale: false, ...extra })

function html(locale: 'en' | 'pl', analysis: Analysis) {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <ProblemsDrawer analysis={analysis} open onFocus={() => undefined} onRetry={() => undefined} />
    </I18nProvider>,
  )
}

describe('the parse issue position', () => {
  it('shows line and column when the engine located the failing node', () => {
    expect(html('en', parsed({ parseLine: 6, parseColumn: 5 }))).toContain('line 6, column 5')
    expect(html('pl', parsed({ parseLine: 6, parseColumn: 5 }))).toContain('linia 6, kolumna 5')
  })
  it('shows the line alone when there is no column, and never invents one', () => {
    const out = html('en', parsed({ parseLine: 3 }))
    expect(out).toContain('line 3')
    expect(out).not.toContain('column')
  })
  it('shows no position when the engine named none', () => {
    const out = html('en', parsed({}))
    expect(out).not.toContain('mu mono')
    expect(out).not.toContain('column')
  })
})
