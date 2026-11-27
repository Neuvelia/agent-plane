import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { AboutDialog, ShortcutsDialog, aboutStatusKey, copyText, diagnosticsText } from '../src/ui/dialogs'

const noop = () => undefined
const wrap = (node: React.ReactNode, locale: 'en' | 'pl' = 'en') => renderToStaticMarkup(<I18nProvider locale={locale}>{node}</I18nProvider>)
const text = (html: string) => html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()

describe('Keyboard shortcuts dialog (F2.5c)', () => {
  it('lists only the shortcuts the app has, written as text [F2.5c-AC8]', () => {
    const out = wrap(<ShortcutsDialog onClose={noop} />)
    const rows = [...out.matchAll(/<div class="sc">([\s\S]*?)<\/div>/g)].map((m) => text(m[1]!))
    // The command palette's shortcuts (F2.5d) are among the ones the app has.
    expect(rows).toEqual([
      'Command palette Ctrl K',
      'New run Ctrl N',
      'Open a graph Ctrl O',
      'Review approvals Ctrl Shift A',
      'Settings Ctrl ,',
      'This list In the YAML editor Ctrl / comments the line instead. Ctrl /',
      'Go to the Inbox G, then I within a second, when no text field has focus. G I',
      'Save the graph Ctrl S',
      'Move between menu items ↑ ↓',
      'Close a menu or dialog, cancel an inline edit Esc',
    ])
    expect(out).toContain('aria-labelledby="shortcuts-title"')
    expect(out).toMatch(/<button[^>]*data-testid="shortcuts-dialog-close"/)
    expect(out).not.toContain('<svg')
  })
  it('is localised', () => {
    const out = text(wrap(<ShortcutsDialog onClose={noop} />, 'pl'))
    expect(out).toContain('Skróty klawiszowe')
    expect(out).toContain('Zamknij')
  })
})

describe('About dialog (F2.5c)', () => {
  it('shows the app version and checks the engine and run count, with Copy diagnostics before Close [F2.5c-AC8]', () => {
    const out = wrap(<AboutDialog appVersion="0.14.0" onClose={noop} />)
    expect(out).toContain('aria-labelledby="about-title"')
    expect(text(out)).toContain('Everything stays local: nothing is sent anywhere.')
    expect(out).toMatch(/data-testid="about-app">0\.14\.0</)
    expect(out).toMatch(/data-testid="about-engine">Checking…</)
    expect(out.indexOf('about-copy')).toBeLessThan(out.indexOf('about-dialog-close'))
    // Nothing is copied while the values are still being checked.
    expect(out).toMatch(/<button[^>]*disabled=""[^>]*data-testid="about-copy"/)
  })
  it('puts only versions and a count on the clipboard, never a path', () => {
    const engine = { state: 'ok', value: { version: '0.14.0', os: 'linux', arch: 'amd64' } } as const
    expect(diagnosticsText('0.14.0', engine, { state: 'ok', value: 512 })).toBe('Tarmind 0.14.0\nEngine 0.14.0 linux/amd64\nRuns stored 512')
    expect(diagnosticsText('', { state: 'failed' }, { state: 'loading' })).toBe('Tarmind unknown\nEngine unknown\nRuns stored unknown')
  })
})

describe('About values that are not values (review N3)', () => {
  const ok = { state: 'ok', value: 1 } as const
  it('say Checking while loading, Engine not connected when the engine is down, Not available when only this read failed', () => {
    expect(aboutStatusKey({ state: 'loading' }, ok)).toBe('about.checking')
    expect(aboutStatusKey({ state: 'failed' }, { state: 'failed' })).toBe('about.unreachable')
    expect(aboutStatusKey({ state: 'failed' }, ok)).toBe('about.unavailable')
  })
})

describe('Copy diagnostics (review N3)', () => {
  it('reports copied when the clipboard takes the text', async () => {
    const written: string[] = []
    expect(await copyText('x', () => ({ writeText: async (s: string) => void written.push(s) }))).toBe('copied')
    expect(written).toEqual(['x'])
  })
  it('reports failed, never throws, when the clipboard refuses or is missing', async () => {
    expect(await copyText('x', () => ({ writeText: () => Promise.reject(new Error('denied')) }))).toBe('failed')
    expect(await copyText('x', () => undefined)).toBe('failed')
    expect(
      await copyText('x', () => {
        throw new TypeError('navigator.clipboard is undefined')
      }),
    ).toBe('failed')
  })
})
