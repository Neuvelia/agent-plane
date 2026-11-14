import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { Sidebar, type NavEntry } from '../src/ui/shell'

const active: NavEntry[] = [{ id: 'runs', icon: 'runs', label: 'nav.runs', active: true, onSelect: () => undefined }]
const deferred: NavEntry[] = ['projects', 'runners', 'docs', 'settings'].map((id) => ({ id, icon: id as 'projects', label: `nav.${id}` as 'nav.projects' }))

function html(locale: 'en' | 'pl') {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <Sidebar entries={active} deferred={deferred} />
    </I18nProvider>,
  )
}

describe('Sidebar deferred entries', () => {
  it('renders each deferred surface as aria-disabled, non-interactive, with a Soon tag', () => {
    const out = html('en')
    for (const id of ['projects', 'runners', 'docs', 'settings']) {
      const m = new RegExp(`<div[^>]*data-testid="nav-${id}"[^>]*>`).exec(out)
      expect(m, id).not.toBeNull()
      expect(m![0]).toContain('aria-disabled="true"')
      expect(m![0]).not.toContain('tabindex')
      // Not a button or link, so it cannot take focus or fire navigation.
      expect(out).not.toMatch(new RegExp(`<(button|a)[^>]*data-testid="nav-${id}"`))
    }
    expect((out.match(/Soon/g) ?? []).length).toBe(4)
  })
  it('localises entries and the Soon tag', () => {
    const out = html('pl')
    expect(out).toContain('Wkrótce')
    expect(out).toContain('Projekty')
    expect(out).not.toContain('>Soon<')
  })
  it('keeps the active entry a real button marked as current', () => {
    expect(html('en')).toMatch(/<button[^>]*aria-current="page"[^>]*data-testid="nav-runs"/)
  })
})
