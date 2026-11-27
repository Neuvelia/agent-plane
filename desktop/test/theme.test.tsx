import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { Settings } from '../src/settings'
import { THEME_PREFS, applyTheme, loadThemePref, resolveTheme, saveThemePref } from '../src/theme'

// Light and dark themes (F2.5b): a per-viewer preference, resolved against the
// operating system, written to <html data-theme>.

class MemoryStorage {
  private m = new Map<string, string>()
  getItem(k: string) {
    return this.m.get(k) ?? null
  }
  setItem(k: string, v: string) {
    this.m.set(k, v)
  }
  removeItem(k: string) {
    this.m.delete(k)
  }
}

const g = globalThis as { localStorage?: unknown }
afterEach(() => {
  delete g.localStorage
})

describe('theme preference', () => {
  it('defaults to system and follows the operating system [F2.5b-AC4]', () => {
    expect(loadThemePref()).toBe('system')
    expect(resolveTheme('system', true)).toBe('light')
    expect(resolveTheme('system', false)).toBe('dark')
  })

  it('an explicit choice wins over the operating system', () => {
    expect(resolveTheme('light', false)).toBe('light')
    expect(resolveTheme('dark', true)).toBe('dark')
  })

  it('remembers a choice and ignores an unknown stored value', () => {
    g.localStorage = new MemoryStorage()
    saveThemePref('light')
    expect(loadThemePref()).toBe('light')
    ;(g.localStorage as MemoryStorage).setItem('agentplane.theme', 'neon')
    expect(loadThemePref()).toBe('system')
  })

  it('a throwing store loses only the remembered choice', () => {
    g.localStorage = {
      getItem: () => {
        throw new Error('blocked')
      },
      setItem: () => {
        throw new Error('blocked')
      },
    }
    expect(() => saveThemePref('dark')).not.toThrow()
    expect(loadThemePref()).toBe('system')
  })

  it('writes the resolved theme to the root element', () => {
    const root = { dataset: {} as Record<string, string> } as unknown as HTMLElement
    applyTheme('light', root)
    expect(root.dataset.theme).toBe('light')
  })

  it('Settings offers every preference, in both languages [F2.5b-AC4]', () => {
    for (const locale of ['en', 'pl'] as const) {
      const html = renderToStaticMarkup(
        <I18nProvider locale={locale}>
          <Settings />
        </I18nProvider>,
      )
      for (const p of THEME_PREFS) expect(html).toContain(`data-testid="theme-${p}"`)
    }
  })
})
