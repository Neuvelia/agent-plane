import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode, type Ref, type RefObject } from 'react'
import { useI18n, type LocaleChoice } from '../i18n/context'
import { THEME_CHOICES, useTheme, type ThemeChoice } from '../theme'
import { closesSubmenu, isAppShortcut as appShortcut, modifierLabel, moveFocus, openFromButton } from './menuKeys'
import { usePresence } from './presence'

// F2.5b profile menu: the menu under the avatar at the foot of the sidebar,
// drawn 1:1 from the design's "Profile menu" artboard. Only what exists in a
// local app: no account, no plan, no organisation, no Log out.

/** Placeholder identity until accounts exist (F2.5a): initials and name are hard-coded. */
export const PROFILE_INITIALS = 'KW'
export const PROFILE_NAME = 'Kacper'

/** Exit animation lengths; styles.css `.pm.closing` and `.pm-sub.closing` use the same values. */
const MENU_EXIT_MS = 120
const SUB_EXIT_MS = 100
/** The submenu's padding plus border: its last item lines up with the bottom of its trigger. */
const SUB_INSET = 7

type Sub = 'language' | 'appearance'
export type ProfileMenuState = 'closed' | 'open' | Sub

/** Native language names are never translated; "System" is. */
const LANGUAGE_NAME = { en: 'English', pl: 'Polski' } as const
const LANGUAGE_CHOICES: LocaleChoice[] = ['system', 'en', 'pl']

export interface ProfileMenuProps {
  onSettings: () => void
  onShortcuts: () => void
  onAbout: () => void
  appVersion: string
  /** Storybook and tests: the state the menu starts in. */
  initial?: ProfileMenuState
}

function platform(): string {
  return typeof navigator === 'undefined' ? '' : navigator.platform || navigator.userAgent
}

export function ProfileMenu({ onSettings, onShortcuts, onAbout, appVersion, initial = 'closed' }: ProfileMenuProps) {
  const { t, choice: langChoice, setChoice: setLang } = useI18n()
  const { choice: themeChoice, setChoice: setTheme } = useTheme()
  const [open, setOpen] = useState(initial !== 'closed')
  const [sub, setSub] = useState<Sub | null>(initial === 'language' || initial === 'appearance' ? initial : null)
  const menu = usePresence(open, MENU_EXIT_MS)
  const submenu = usePresence(open && sub !== null, SUB_EXIT_MS)
  // The submenu keeps its last content while it animates out.
  const [shownSub, setShownSub] = useState<Sub | null>(sub)
  if (sub !== null && sub !== shownSub) setShownSub(sub)

  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const subRef = useRef<HTMLDivElement>(null)
  const pendingFocus = useRef<'first' | 'last' | Sub | 'sub' | null>(null)
  const menuId = useId()
  const subId = useId()
  const mod = modifierLabel(platform())

  const close = useCallback((returnFocus: boolean) => {
    setOpen(false)
    setSub(null)
    if (returnFocus) buttonRef.current?.focus()
  }, [])

  // A press outside the menu, its submenu and its button closes it, as Tab away does.
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) close(false)
    }
    document.addEventListener('pointerdown', onDown)
    return () => document.removeEventListener('pointerdown', onDown)
  }, [open, close])

  // An app shortcut (Ctrl/Cmd+, or /) closes the menu wherever focus is, even outside it
  // (review N1). From inside the menu the key handlers below also return focus to the button.
  useEffect(() => {
    if (!open) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (appShortcut(e, platform())) close(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, close])

  // Focus moves once the element it targets is in the DOM.
  useEffect(() => {
    const target = pendingFocus.current
    if (!target) return
    pendingFocus.current = null
    if (target === 'sub') return focusChecked(subRef.current)
    if (target === 'language' || target === 'appearance') return menuRef.current?.querySelector<HTMLElement>(`[data-sub="${target}"]`)?.focus()
    const items = itemsOf(menuRef.current)
    items[target === 'first' ? 0 : items.length - 1]?.focus()
  })

  const openMenu = (focus: 'first' | 'last') => {
    pendingFocus.current = focus
    setOpen(true)
  }
  const openSub = (s: Sub, focus: boolean) => {
    // Already open (by pointer): no state changes and no render follows, so move focus now.
    if (sub === s) {
      if (focus) focusChecked(subRef.current)
      return
    }
    pendingFocus.current = focus ? 'sub' : null
    setSub(s)
  }
  const closeSub = () => {
    pendingFocus.current = sub
    setSub(null)
  }
  // The button gets focus back first; a dialog opened by `run` then takes it and returns it on close.
  const act = (run: () => void) => () => {
    close(true)
    run()
  }
  // Enter and Space reach a trigger as a click with no pointer (detail 0): that always opens the
  // submenu and moves focus into it. A pointer click toggles it and leaves focus where it is.
  const toggleSub = (s: Sub) => (e: MouseEvent) => {
    if (e.detail === 0) return openSub(s, true)
    if (sub !== s) return openSub(s, false)
    pendingFocus.current = null
    setSub(null)
  }

  const onButtonKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    const focus = openFromButton(e.key)
    if (!focus || open) return
    e.preventDefault()
    openMenu(focus)
  }
  const onMenuKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Tab') return close(false)
    // An app shortcut (Ctrl/Cmd+, or /) closes the menu onto the button; the key still reaches the app.
    if (isAppShortcut(e)) return close(true)
    if (e.key === 'Escape') {
      e.preventDefault()
      return close(true)
    }
    const s = (e.target as HTMLElement).dataset.sub as Sub | undefined
    if (s && e.key === 'ArrowRight') {
      e.preventDefault()
      return openSub(s, true)
    }
    if (roveFocus(e, menuRef.current)) setSub(null)
  }
  const onSubKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Tab') return close(false)
    if (isAppShortcut(e)) return close(true)
    if (closesSubmenu(e.key)) {
      e.preventDefault()
      e.stopPropagation()
      return closeSub()
    }
    roveFocus(e, subRef.current)
  }

  const languageName = (c: LocaleChoice) => (c === 'system' ? t('choice.system') : LANGUAGE_NAME[c])
  const themeName = (c: ThemeChoice) => (c === 'system' ? t('choice.system') : t(c === 'light' ? 'theme.light' : 'theme.dark'))
  const pickLanguage = (c: LocaleChoice) => {
    setLang(c)
    closeSub()
  }
  const pickTheme = (c: ThemeChoice) => {
    setTheme(c)
    closeSub()
  }

  return (
    <div className="profile" ref={rootRef}>
      {menu.mounted && (
        <div className={`pm ${menu.phase}`} role="menu" id={menuId} aria-label={t('profile.menu')} ref={menuRef} onKeyDown={onMenuKey} data-testid="profile-menu">
          <div className="pm-who" role="none">
            <b>{PROFILE_NAME}</b>
            <span>{t('profile.stored')}</span>
          </div>
          <hr />
          <Item icon="settings" label={t('profile.settings')} kbd={`${mod} ,`} onClick={act(onSettings)} testId="profile-settings" />
          <Item icon="shortcuts" label={t('profile.shortcuts')} kbd={`${mod} /`} onClick={act(onShortcuts)} testId="profile-shortcuts" />
          <hr />
          <SubTrigger sub="language" icon="language" label={t('profile.language')} value={languageName(langChoice)} open={sub === 'language'} controls={subId} onToggle={toggleSub('language')} />
          <SubTrigger sub="appearance" icon="appearance" label={t('profile.appearance')} value={themeName(themeChoice)} open={sub === 'appearance'} controls={subId} onToggle={toggleSub('appearance')} />
          <hr />
          <Item icon="help" label={t('profile.help')} right={t('nav.soon')} disabled testId="profile-help" />
          <Item icon="about" label={t('profile.about')} right={appVersion || undefined} onClick={act(onAbout)} testId="profile-about" />
        </div>
      )}
      {menu.mounted && submenu.mounted && shownSub && (
        <SubMenu id={subId} label={t(shownSub === 'language' ? 'profile.language' : 'profile.appearance')} phase={submenu.phase} sub={shownSub} menuRef={menuRef} ref={subRef} onKeyDown={onSubKey}>
          {shownSub === 'language'
            ? LANGUAGE_CHOICES.map((c) => <Radio key={c} label={languageName(c)} checked={langChoice === c} onPick={() => pickLanguage(c)} testId={`profile-language-${c}`} />)
            : THEME_CHOICES.map((c) => <Radio key={c} label={themeName(c)} checked={themeChoice === c} onPick={() => pickTheme(c)} testId={`profile-theme-${c}`} />)}
        </SubMenu>
      )}
      <button
        type="button"
        className="me"
        ref={buttonRef}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => (open ? close(false) : openMenu('first'))}
        onKeyDown={onButtonKey}
        data-testid="profile"
      >
        <span className="av" aria-hidden="true">
          {PROFILE_INITIALS}
        </span>
        <span className="me-name">{PROFILE_NAME}</span>
        <span className="me-local">{t('profile.local')}</span>
      </button>
    </div>
  )
}

function isAppShortcut(e: KeyboardEvent<HTMLElement>): boolean {
  return appShortcut(e, platform()) !== null
}

function itemsOf(container: HTMLElement | null): HTMLElement[] {
  return container ? [...container.querySelectorAll<HTMLElement>('[role^="menuitem"]')] : []
}

function focusChecked(container: HTMLElement | null) {
  const items = itemsOf(container)
  ;(items.find((i) => i.getAttribute('aria-checked') === 'true') ?? items[0])?.focus()
}

/** Moves focus for arrow, Home and End keys; true when it handled the key. */
function roveFocus(e: KeyboardEvent<HTMLElement>, container: HTMLElement | null): boolean {
  const items = itemsOf(container)
  const next = moveFocus(e.key, items.indexOf(document.activeElement as HTMLElement), items.length)
  if (next === null) return false
  e.preventDefault()
  items[next]?.focus()
  return true
}

type MenuIcon = 'settings' | 'shortcuts' | 'language' | 'appearance' | 'help' | 'about'

// Stroke glyphs copied from the design's artboard; decorative, the label is beside them.
const GLYPH: Record<MenuIcon, ReactNode> = {
  settings: (
    <>
      <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
      <circle cx="15" cy="7" r="2" />
      <circle cx="9" cy="17" r="2" />
    </>
  ),
  shortcuts: (
    <>
      <rect x="3" y="6" width="18" height="12" rx="2.5" />
      <path d="M7 10h.01M11 10h.01M15 10h.01M7 14h10" />
    </>
  ),
  language: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" />
    </>
  ),
  appearance: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 3v18" />
      <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" />
    </>
  ),
  help: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.7.4-1 .9-1 1.7M12 17h.01" />
    </>
  ),
  about: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8h.01" />
    </>
  ),
}

function Glyph({ name }: { name: MenuIcon }) {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true" focusable="false">
      {GLYPH[name]}
    </svg>
  )
}

function Chevron() {
  return (
    <svg className="chev" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      <path d="m9 6 6 6-6 6" />
    </svg>
  )
}

function Item({ icon, label, kbd, right, onClick, disabled, testId }: { icon: MenuIcon; label: string; kbd?: string; right?: string; onClick?: () => void; disabled?: boolean; testId: string }) {
  return (
    <button type="button" className="mi" role="menuitem" tabIndex={-1} aria-disabled={disabled || undefined} onClick={disabled ? undefined : onClick} data-testid={testId}>
      <Glyph name={icon} />
      {label}
      {kbd && (
        <span className="kb" aria-hidden="true">
          {kbd}
        </span>
      )}
      {right && <span className="r">{right}</span>}
    </button>
  )
}

function SubTrigger({ sub, icon, label, value, open, controls, onToggle }: { sub: Sub; icon: MenuIcon; label: string; value: string; open: boolean; controls: string; onToggle: (e: MouseEvent) => void }) {
  return (
    <button type="button" className="mi" role="menuitem" tabIndex={-1} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? controls : undefined} data-sub={sub} onClick={onToggle} data-testid={`profile-${sub}`}>
      <Glyph name={icon} />
      {label}
      <span className="r">
        {value}
        <Chevron />
      </span>
    </button>
  )
}

function Radio({ label, checked, onPick, testId }: { label: string; checked: boolean; onPick: () => void; testId: string }) {
  return (
    <button type="button" className="mi" role="menuitemradio" tabIndex={-1} aria-checked={checked} onClick={onPick} data-testid={testId}>
      {label}
      <svg className="ck" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
        <path d="M5 12.5 10 17.5 19 7" />
      </svg>
    </button>
  )
}

interface SubMenuProps {
  id: string
  label: string
  phase: string
  sub: Sub
  menuRef: RefObject<HTMLDivElement | null>
  onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => void
  children: ReactNode
  ref: Ref<HTMLDivElement>
}

/** The submenu sits to the right of the menu; its last item lines up with the bottom of its trigger. */
function SubMenu({ id, label, phase, sub, menuRef, onKeyDown, children, ref }: SubMenuProps) {
  const [bottom, setBottom] = useState<number | null>(null)
  // Measured after commit, before paint, from layout offsets: the menu may still be
  // scaling in, and a transform would skew getBoundingClientRect.
  useLayoutEffect(() => {
    // The menu's offset parent is the profile root; on first mount the root's own
    // ref is attached only after this child effect runs, so it is not used here.
    const menu = menuRef.current
    const anchor = menu?.querySelector<HTMLElement>(`[data-sub="${sub}"]`)
    const root = menu?.offsetParent
    if (!menu || !anchor || !root) return
    const anchorBottom = menu.offsetTop + menu.clientTop + anchor.offsetTop + anchor.offsetHeight
    setBottom(root.clientHeight - anchorBottom - SUB_INSET)
  }, [sub, menuRef])
  return (
    <div className={`pm-sub ${phase}`} role="menu" id={id} aria-label={label} ref={ref} onKeyDown={onKeyDown} style={bottom === null ? undefined : { bottom }} data-testid="profile-submenu">
      {children}
    </div>
  )
}
