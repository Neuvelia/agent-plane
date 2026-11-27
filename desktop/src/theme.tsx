import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

// F2.5b (ADR-0046): the interface theme. The choice is a renderer preference in
// localStorage, like the language; "system" follows the operating system.
// Tokens for each theme live in design/tokens.css under :root[data-theme].

export type Theme = 'dark' | 'light'
export type ThemeChoice = 'system' | Theme
export const THEME_CHOICES: ThemeChoice[] = ['system', 'light', 'dark']

const STORAGE_KEY = 'agentplane.theme'
const DARK_QUERY = '(prefers-color-scheme: dark)'
/** How long the colour cross-fade lasts when the theme changes (styles.css `.theme-switching`). */
export const THEME_FADE_MS = 240

/** Reads the saved choice. Missing, unknown or throwing storage yields "system". */
export function loadThemeChoice(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): ThemeChoice {
  try {
    const v = storage?.getItem(STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

/** Saves the choice; "system" removes the key so a later OS change is followed. */
export function saveThemeChoice(choice: ThemeChoice, storage: Pick<Storage, 'setItem' | 'removeItem'> | undefined = safeStorage()): void {
  try {
    if (choice === 'system') storage?.removeItem(STORAGE_KEY)
    else storage?.setItem(STORAGE_KEY, choice)
  } catch {
    // Unavailable storage: the choice lasts for the session only.
  }
}

/** The theme a choice resolves to. Without a media query answer the app stays dark, its original look. */
export function resolveTheme(choice: ThemeChoice, systemPrefersDark: boolean | undefined): Theme {
  if (choice !== 'system') return choice
  return systemPrefersDark === false ? 'light' : 'dark'
}

function safeStorage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage
  } catch {
    return undefined
  }
}

function systemPrefersDark(): boolean | undefined {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined
  return window.matchMedia(DARK_QUERY).matches
}

/** Puts the theme on <html>, where the token overrides and color-scheme read it. */
export function applyTheme(theme: Theme, root: HTMLElement | undefined = typeof document === 'undefined' ? undefined : document.documentElement): void {
  if (root) root.dataset.theme = theme
}

/** The theme to paint before React mounts, so a light choice never flashes dark. */
export function initialTheme(): Theme {
  return resolveTheme(loadThemeChoice(), systemPrefersDark())
}

interface ThemeState {
  choice: ThemeChoice
  theme: Theme
  setChoice: (c: ThemeChoice) => void
}

const Ctx = createContext<ThemeState | null>(null)

/** Holds the theme choice. `initialChoice` (Storybook, tests) replaces the saved one at mount. */
export function ThemeProvider({ children, initialChoice }: { children: ReactNode; initialChoice?: ThemeChoice }) {
  const [choice, setChoiceState] = useState<ThemeChoice>(() => initialChoice ?? loadThemeChoice())
  const [prefersDark, setPrefersDark] = useState<boolean | undefined>(systemPrefersDark)
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    const query = window.matchMedia(DARK_QUERY)
    const onChange = () => setPrefersDark(query.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  const theme = resolveTheme(choice, prefersDark)
  // Cross-fade only when the painted theme really changes, whether a pick or the OS changed it;
  // never on the first paint. A second change during a fade restarts its timer.
  const painted = useRef<Theme | null>(null)
  const fadeTimer = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (painted.current !== null && painted.current !== theme) fadeTimer.current = crossFade(fadeTimer.current)
    painted.current = theme
    applyTheme(theme)
  }, [theme])
  useEffect(() => () => window.clearTimeout(fadeTimer.current), [])
  const setChoice = useCallback((c: ThemeChoice) => {
    saveThemeChoice(c)
    setChoiceState(c)
  }, [])
  const value = useMemo<ThemeState>(() => ({ choice, theme, setChoice }), [choice, theme, setChoice])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

/** Colours cross-fade for a moment instead of snapping; skipped under reduced motion. Returns the new timer. */
function crossFade(previous: number | undefined): number | undefined {
  window.clearTimeout(previous)
  if (typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches) return undefined
  const root = document.documentElement
  root.classList.add('theme-switching')
  return window.setTimeout(() => root.classList.remove('theme-switching'), THEME_FADE_MS)
}

/** Dot colour of the graph canvases' background grid in each theme. */
export const GRID_DOT: Record<Theme, string> = { dark: '#242424', light: '#D9D9DE' }

/** The painted theme; dark outside a provider (unit tests, isolated renders). */
export function useResolvedTheme(): Theme {
  return useContext(Ctx)?.theme ?? 'dark'
}

export function useTheme(): ThemeState {
  const v = useContext(Ctx)
  if (!v) throw new Error('useTheme must be used inside <ThemeProvider>')
  return v
}
