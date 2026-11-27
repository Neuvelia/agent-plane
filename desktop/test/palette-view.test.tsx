import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { SHORTCUTS } from '../src/palette'
import { CommandPalette, type PaletteCommand, type RunsState } from '../src/ui/CommandPalette'
import { GlobalBar } from '../src/ui/shell'

const noop = () => undefined
const commands: PaletteCommand[] = [
  { id: 'new-run', group: 'actions', label: 'New run', hint: '', shortcut: SHORTCUTS.newRun, icon: 'runs', perform: noop },
  { id: 'approvals', group: 'actions', label: 'Review approvals', hint: '2 waiting', shortcut: SHORTCUTS.approvals, icon: 'approvals', perform: noop },
  { id: 'run:8c1f40a2-bbbb', group: 'runs', label: 'release-notes', hint: '8c1f40a2 · running', runId: '8c1f40a2-bbbb', icon: 'runs', perform: noop },
  { id: 'go-settings', group: 'goto', label: 'Settings', hint: '', shortcut: SHORTCUTS.settings, icon: 'settings', perform: noop },
]

function html({ query = '', runs = 'ready' as RunsState, mac = false, locale = 'en' as 'en' | 'pl' } = {}) {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <CommandPalette commands={commands} runs={runs} mac={mac} onClose={noop} initialQuery={query} />
    </I18nProvider>,
  )
}

describe('CommandPalette', () => {
  // roadmap: F2.5b-AC2
  it('is a labelled dialog with a combobox that points at the first option', () => {
    const out = html()
    expect(out).toMatch(/<dialog[^>]*aria-label="Command palette"/)
    const input = /<input[^>]*role="combobox"[^>]*>/.exec(out)![0]
    expect(input).toContain('aria-expanded="true"')
    const active = /aria-activedescendant="([^"]+)"/.exec(input)![1]
    expect(out).toMatch(new RegExp(`id="${active}"[^>]*role="option"[^>]*aria-selected="true"[^>]*data-testid="palette-item-new-run"`))
    expect((out.match(/aria-selected="true"/g) ?? []).length).toBe(1)
  })
  it('groups options under their headings in order', () => {
    const out = html()
    const order = ['Actions', 'New run', 'Review approvals', 'Runs', 'release-notes', 'Go to', 'Settings'].map((s) => out.indexOf(`>${s}<`))
    expect(order.every((i) => i >= 0)).toBe(true)
    expect([...order].sort((a, b) => a - b)).toEqual(order)
    expect((out.match(/role="group"/g) ?? []).length).toBe(3)
  })
  it('shows hints and shortcuts in the platform spelling', () => {
    expect(html()).toContain('2 waiting<kbd>Ctrl Shift A</kbd>')
    expect(html({ mac: true })).toContain('2 waiting<kbd>⌘ ⇧ A</kbd>')
  })
  // roadmap: F2.5b-AC4
  it('says nothing matches, without an empty listbox', () => {
    const out = html({ query: 'zzz' })
    expect(out).toContain('Nothing matches “zzz”. Try a run id or a graph name.')
    expect(out).not.toContain('role="listbox"')
    expect(out).toContain('aria-expanded="false"')
    expect(out).not.toContain('aria-activedescendant')
  })
  // roadmap: F2.5b-AC4
  it('reports runs that are loading or could not be read, but not for the actions scope', () => {
    expect(html({ runs: 'loading' })).toContain('Loading runs…')
    expect(html({ runs: 'error' })).toMatch(/class="cmdk-note bad"[^>]*>Runs could not be loaded/)
    expect(html({ runs: 'error', query: '>' })).not.toContain('Runs could not be loaded')
  })
  it('is translated', () => {
    const out = html({ locale: 'pl' })
    expect(out).toContain('aria-label="Paleta poleceń"')
    expect(out).toContain('placeholder="Szukaj uruchomień, grafów i akcji"')
    expect(out).toContain('>Akcje<')
  })
})

describe('GlobalBar search', () => {
  // roadmap: F2.5b-AC1
  it('offers the palette as a button that names its shortcut', () => {
    const out = renderToStaticMarkup(
      <I18nProvider locale="en">
        <GlobalBar lost={false} search={{ onOpen: noop, keys: 'Ctrl K', ariaKeys: 'Control+K' }} />
      </I18nProvider>,
    )
    const button = /<button[^>]*data-testid="palette-open"[^>]*>Search<\/button>/.exec(out)
    expect(button).not.toBeNull()
    expect(button![0]).toContain('aria-label="Search and commands"')
    expect(button![0]).toContain('aria-keyshortcuts="Control+K"')
    expect(button![0]).toContain('title="Search and commands (Ctrl K)"')
  })
  it('has no search button without a trigger', () => {
    const out = renderToStaticMarkup(
      <I18nProvider locale="en">
        <GlobalBar lost={false} />
      </I18nProvider>,
    )
    expect(out).not.toContain('palette-open')
  })
})
