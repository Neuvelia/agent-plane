import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { clearLocale, loadLocale, saveLocale } from '../src/i18n'
import { THEME_FADE_MS, ThemeProvider, crossFade, loadThemeChoice, resolveTheme, saveThemeChoice, type ThemeChoice } from '../src/theme'
import { closesSubmenu, modifierLabel, moveFocus, openFromButton } from '../src/ui/menuKeys'
import { IDLE, matchShortcut } from '../src/palette'
import { PROFILE_NAME, ProfileMenu, type ProfileMenuState } from '../src/ui/profileMenu'
import { PROFILE_INITIALS, Sidebar } from '../src/ui/shell'

const noop = () => undefined

function html(initial: ProfileMenuState, locale: 'en' | 'pl' = 'en', theme: ThemeChoice = 'dark') {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <ThemeProvider initialChoice={theme}>
        <Sidebar entries={[]} deferred={[]} profile={<ProfileMenu appVersion="0.14.0" initial={initial} onSettings={noop} onShortcuts={noop} onAbout={noop} />} />
      </ThemeProvider>
    </I18nProvider>,
  )
}

/** Visible text of each menu item, in order. */
function items(out: string, role: string): string[] {
  return [...out.matchAll(new RegExp(`<button[^>]*role="${role}"[^>]*>([\\s\\S]*?)</button>`, 'g'))].map((m) => m[1]!.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim())
}

class MemoryStorage {
  data = new Map<string, string>()
  getItem = (k: string) => this.data.get(k) ?? null
  setItem = (k: string, v: string) => void this.data.set(k, v)
  removeItem = (k: string) => void this.data.delete(k)
}

describe('profile row (F2.5c)', () => {
  it('is a closed menu button at the foot of the sidebar with initials, name and Local [F2.5c-AC1]', () => {
    const out = html('closed')
    const button = /<button[^>]*data-testid="profile"[^>]*>/.exec(out)?.[0] ?? ''
    expect(button).toContain('aria-haspopup="menu"')
    expect(button).toContain('aria-expanded="false"')
    expect(out).toMatch(/class="side-foot"[\s\S]*data-testid="profile"/)
    expect(out).toContain(`>${PROFILE_INITIALS}<`)
    expect(out).toContain(`>${PROFILE_NAME}<`)
    expect(out).toContain('>Local<')
    expect(out).not.toContain('role="menu"')
  })
})

describe('profile menu (F2.5c)', () => {
  it('draws the design: header, then Settings and shortcuts, Language and Appearance, Help and About [F2.5c-AC2]', () => {
    const out = html('open')
    expect(out).toMatch(/<button[^>]*aria-expanded="true"[^>]*data-testid="profile"/)
    expect(out).toContain('role="menu" id=')
    expect(out).toContain('aria-label="Profile"')
    expect(out).toContain('Local profile · stored on this computer')
    expect(items(out, 'menuitem')).toEqual(['Settings Ctrl ,', 'Keyboard shortcuts Ctrl /', 'Language English', 'Appearance Dark', 'Help and documentation Soon', 'About Tarmind 0.14.0'])
    // One separator under the header and one between each of the three groups.
    expect((out.match(/<hr\/>/g) ?? []).length).toBe(3)
    for (const absent of ['Log out', 'Upgrade', 'Organisation', 'Sign']) expect(out).not.toContain(absent)
  })
  it('marks Help as unavailable rather than hiding it', () => {
    expect(html('open')).toMatch(/<button[^>]*aria-disabled="true"[^>]*data-testid="profile-help"/)
  })
  it('is localised', () => {
    const out = html('open', 'pl')
    expect(items(out, 'menuitem')).toEqual(['Ustawienia Ctrl ,', 'Skróty klawiszowe Ctrl /', 'Język Polski', 'Wygląd Ciemny', 'Pomoc i dokumentacja Wkrótce', 'O aplikacji Tarmind 0.14.0'])
  })
  it('offers System, English and Polski in the Language submenu, one checked [F2.5c-AC3]', () => {
    const out = html('language', 'pl')
    expect(out).toMatch(/<button[^>]*aria-expanded="true"[^>]*data-sub="language"/)
    expect(items(out, 'menuitemradio')).toEqual(['Systemowy', 'English', 'Polski'])
    expect(out.match(/aria-checked="true"/g)?.length).toBe(1)
    expect(out).toMatch(/aria-checked="true"[^>]*data-testid="profile-language-pl"/)
  })
  it('offers System, Light and Dark in the Appearance submenu with the current choice checked [F2.5c-AC4]', () => {
    const out = html('appearance', 'en', 'light')
    expect(items(out, 'menuitemradio')).toEqual(['System', 'Light', 'Dark'])
    expect(out).toMatch(/aria-checked="true"[^>]*data-testid="profile-theme-light"/)
    expect(items(html('appearance', 'en', 'system'), 'menuitem')).toContain('Appearance System')
  })
})

describe('menu keyboard model', () => {
  it('roves with arrows, Home and End, wrapping at both ends [F2.5c-AC5]', () => {
    expect(moveFocus('ArrowDown', 0, 6)).toBe(1)
    expect(moveFocus('ArrowDown', 5, 6)).toBe(0)
    expect(moveFocus('ArrowUp', 0, 6)).toBe(5)
    expect(moveFocus('ArrowUp', -1, 6)).toBe(5)
    expect(moveFocus('ArrowDown', -1, 6)).toBe(0)
    expect(moveFocus('Home', 3, 6)).toBe(0)
    expect(moveFocus('End', 3, 6)).toBe(5)
    expect(moveFocus('a', 3, 6)).toBeNull()
    expect(moveFocus('ArrowDown', 0, 0)).toBeNull()
  })
  it('opens from the button with arrows (Enter and Space click it) and closes a submenu with ArrowLeft or Escape', () => {
    expect(openFromButton('ArrowDown')).toBe('first')
    expect(openFromButton('ArrowUp')).toBe('last')
    expect(openFromButton('Enter')).toBeNull()
    expect(closesSubmenu('ArrowLeft')).toBe(true)
    expect(closesSubmenu('Escape')).toBe(true)
    expect(closesSubmenu('ArrowRight')).toBe(false)
  })
  it('writes and reads shortcuts with Cmd on macOS and Ctrl elsewhere', () => {
    expect(modifierLabel('MacIntel')).toBe('Cmd')
    expect(modifierLabel('Linux x86_64')).toBe('Ctrl')
    expect(modifierLabel('Win32')).toBe('Ctrl')
  })
})

// Ctrl/Cmd+, and Ctrl/Cmd+/ are matched by the shell's one shortcut model (palette.ts, F2.5d).
describe('app shortcuts', () => {
  const key = (k: string, extra: Partial<KeyboardEvent> = {}) => ({ key: k, ctrlKey: true, metaKey: false, shiftKey: false, altKey: false, typing: false, time: 0, ...extra })
  const command = (k: ReturnType<typeof key>, mac: boolean) => matchShortcut(IDLE, k, mac).command
  it('are Ctrl/Cmd with , or / [F2.5c-AC9]', () => {
    expect(command(key(','), false)).toBe('settings')
    expect(command(key('/'), false)).toBe('shortcuts')
    expect(command(key('/', { ctrlKey: false, metaKey: true }), true)).toBe('shortcuts')
    expect(command(key('/'), true)).toBeNull()
    expect(command(key('/', { shiftKey: true }), false)).toBeNull()
    expect(command(key('/', { altKey: true }), false)).toBeNull()
    expect(command(key('s'), false)).toBeNull()
  })
})

describe('appearance and language preferences', () => {
  it('saves Light and Dark, and System removes the saved choice [F2.5c-AC4]', () => {
    const s = new MemoryStorage()
    expect(loadThemeChoice(s)).toBe('system')
    saveThemeChoice('light', s)
    expect(loadThemeChoice(s)).toBe('light')
    saveThemeChoice('system', s)
    expect(s.data.size).toBe(0)
    expect(loadThemeChoice(s)).toBe('system')
  })
  it('ignores unknown or unreadable saved values', () => {
    const s = new MemoryStorage()
    s.setItem('agentplane.theme', 'sepia')
    expect(loadThemeChoice(s)).toBe('system')
    const broken = { getItem: () => { throw new Error('denied') } }
    expect(loadThemeChoice(broken)).toBe('system')
  })
  it('resolves System from the operating system and stays dark when it cannot tell', () => {
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', undefined)).toBe('dark')
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })
  it('language System forgets the saved language [F2.5c-AC3]', () => {
    const s = new MemoryStorage()
    saveLocale('pl', s)
    expect(loadLocale(s)).toBe('pl')
    clearLocale(s)
    expect(loadLocale(s)).toBeUndefined()
  })
})

describe('Settings carries the same Appearance choice', () => {
  it('offers System, Light and Dark, pressed on the current choice [F2.5c-AC9]', async () => {
    const { Settings } = await import('../src/settings')
    const out = renderToStaticMarkup(
      <I18nProvider locale="en">
        <ThemeProvider initialChoice="light">
          <Settings />
        </ThemeProvider>
      </I18nProvider>,
    )
    expect(out).toContain('Appearance')
    expect(out).toMatch(/aria-pressed="true"[^>]*data-testid="settings-theme-light"/)
    expect(out).toMatch(/aria-pressed="false"[^>]*data-testid="settings-theme-system"/)
  })
  it('offers the language System choice too, pressed when nothing is saved', async () => {
    const { Settings } = await import('../src/settings')
    const out = renderToStaticMarkup(
      <I18nProvider>
        <ThemeProvider initialChoice="dark">
          <Settings />
        </ThemeProvider>
      </I18nProvider>,
    )
    expect(out).toMatch(/aria-pressed="true"[^>]*data-testid="settings-language-system"/)
  })
})

describe('motion', () => {
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  it('animates the menu, submenu and windows in and out, cross-fades a theme change, and reduced motion stops all of it [F2.5c-AC6]', () => {
    for (const rule of [
      '.pm{width:248px;z-index:50;transform-origin:0 100%;animation:pm-in .16s',
      '.pm.closing{animation:pm-out .12s',
      'animation:pm-sub-in .14s',
      '.pm-sub.closing{animation:pm-sub-out .1s',
      '.modal[open]{animation:modal-in .16s',
      '.modal.closing{animation:modal-out .14s',
      '.mi[aria-expanded=true] .chev{transform:translateX(2px)}',
    ])
      expect(styles).toContain(rule)
    // The exit lengths in code match the CSS, so an element is never removed mid-animation.
    const menu = readFileSync(new URL('../src/ui/profileMenu.tsx', import.meta.url), 'utf8')
    expect(menu).toContain('const MENU_EXIT_MS = 120')
    expect(menu).toContain('const SUB_EXIT_MS = 100')
    expect(readFileSync(new URL('../src/ui/dialogs.tsx', import.meta.url), 'utf8')).toContain('const EXIT_MS = 140')
    expect(THEME_FADE_MS).toBe(240)
    expect(styles).toContain('transition:background-color .24s ease')
    expect(styles).toMatch(/@media \(prefers-reduced-motion:reduce\)\{\*\{transition:none!important;animation:none!important\}\}/)
  })
})

describe('theme cross-fade timer (review N3)', () => {
  afterEach(() => vi.useRealTimers())
  const root = () => {
    const classes = new Set<string>()
    return { classes, classList: { add: (c: string) => void classes.add(c), remove: (c: string) => void classes.delete(c) } as unknown as DOMTokenList }
  }
  it('keeps the class for the full time again when a second change comes during a fade [F2.5c-AC6]', () => {
    vi.useFakeTimers()
    const r = root()
    const first = crossFade(undefined, r, false)
    vi.advanceTimersByTime(150)
    crossFade(first, r, false)
    vi.advanceTimersByTime(200)
    expect(r.classes.has('theme-switching')).toBe(true)
    vi.advanceTimersByTime(THEME_FADE_MS - 200)
    expect(r.classes.has('theme-switching')).toBe(false)
  })
  it('does not fade under reduced motion', () => {
    vi.useFakeTimers()
    const r = root()
    expect(crossFade(undefined, r, true)).toBeUndefined()
    expect(r.classes.size).toBe(0)
  })
})
