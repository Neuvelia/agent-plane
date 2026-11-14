import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { GlobalBar, PROFILE_INITIALS, Sidebar, type NavEntry } from '../src/ui/shell'

const active: NavEntry[] = [{ id: 'runs', icon: 'runs', label: 'nav.runs', active: true, onSelect: () => undefined }]
const footer: NavEntry[] = [{ id: 'settings', icon: 'settings', label: 'nav.settings', onSelect: () => undefined }]
const deferred: NavEntry[] = ['projects', 'runners', 'docs'].map((id) => ({ id, icon: id as 'projects', label: `nav.${id}` as 'nav.projects' }))

function html(locale: 'en' | 'pl') {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <Sidebar entries={active} deferred={deferred} footer={footer} />
    </I18nProvider>,
  )
}

describe('Sidebar deferred entries', () => {
  it('renders each deferred surface as aria-disabled, non-interactive, with a Soon tag', () => {
    const out = html('en')
    for (const id of ['projects', 'runners', 'docs']) {
      const m = new RegExp(`<div[^>]*data-testid="nav-${id}"[^>]*>`).exec(out)
      expect(m, id).not.toBeNull()
      expect(m![0]).toContain('aria-disabled="true"')
      expect(m![0]).not.toContain('tabindex')
      // Not a button or link, so it cannot take focus or fire navigation.
      expect(out).not.toMatch(new RegExp(`<(button|a)[^>]*data-testid="nav-${id}"`))
    }
    expect((out.match(/Soon/g) ?? []).length).toBe(3)
  })
  it('localises entries and the Soon tag', () => {
    const out = html('pl')
    expect(out).toContain('Wkrótce')
    expect(out).toContain('Projekty')
    expect(out).not.toContain('>Soon<')
  })
  it('puts the Tarmind logo and name at the very top of the sidebar, before every entry', () => {
    const out = html('en')
    const brand = out.indexOf('side-brand')
    expect(brand).toBeGreaterThan(-1)
    expect(out.slice(brand)).toMatch(/^side-brand"><img[^>]*src="[^"]*"[^>]*\/><span>Tarmind<\/span>/)
    expect(brand).toBeLessThan(out.indexOf('data-testid="nav-runs"'))
  })
  it('keeps Settings an enabled, real button', () => {
    expect(html('en')).toMatch(/<button[^>]*data-testid="nav-settings"/)
  })
  it('keeps the active entry a real button marked as current', () => {
    expect(html('en')).toMatch(/<button[^>]*aria-current="page"[^>]*data-testid="nav-runs"/)
  })
})

describe('GlobalBar', () => {
  const bar = (lost: boolean, locale: 'en' | 'pl' = 'en') =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <GlobalBar lost={lost} />
      </I18nProvider>,
    )
  it('does not carry the brand (it lives at the top of the sidebar)', () => {
    const out = bar(false)
    expect(out).not.toContain('<img')
    expect(out).not.toContain('>Tarmind<')
  })
  it('shows the hard-coded profile initials with an accessible name', () => {
    expect(PROFILE_INITIALS).toBe('KW')
    expect(bar(false)).toMatch(/role="img" aria-label="Profile: KW"[^>]*>KW</)
    expect(bar(false, 'pl')).toContain('aria-label="Profil: KW"')
  })
  it('reports the connection state', () => {
    expect(bar(false)).toContain('Engine connected')
    expect(bar(true)).toContain('Connection lost')
  })
  it('has no language control (language lives in Settings)', () => {
    expect(bar(false)).not.toContain('aria-pressed')
  })
})
