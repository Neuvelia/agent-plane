import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

// Light and dark themes (F2.5b). The preference is a per-viewer convenience kept in
// renderer localStorage, like the language; `system` follows the operating system.
// The resolved theme is written to <html data-theme>, where tokens.css switches the
// colour tokens. Nothing here reaches the sidecar.

export const THEME_PREFS = ['system', 'light', 'dark'] as const
export type ThemePref = (typeof THEME_PREFS)[number]
export type Theme = 'light' | 'dark'

const KEY = 'agentplane.theme'

function storage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage
  } catch {
    return undefined
  }
}

/** The saved preference; a missing, unknown or unreadable value is `system`. */
export function loadThemePref(): ThemePref {
  try {
    const v = storage()?.getItem(KEY)
    return (THEME_PREFS as readonly string[]).includes(v ?? '') ? (v as ThemePref) : 'system'
  } catch {
    return 'system'
  }
}

export function saveThemePref(p: ThemePref): void {
  try {
    storage()?.setItem(KEY, p)
  } catch {
    // A blocked store only loses the remembered choice.
  }
}

/** `system` resolves through the operating system's preference; dark when it is unknown. */
export function resolveTheme(pref: ThemePref, systemPrefersLight: boolean): Theme {
  if (pref === 'system') return systemPrefersLight ? 'light' : 'dark'
  return pref
}

const query = () => (typeof matchMedia === 'undefined' ? undefined : matchMedia('(prefers-color-scheme: light)'))

/** Writes the theme to the document root, where the colour tokens switch. */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  root.dataset.theme = theme
}

interface ThemeState {
  pref: ThemePref
  theme: Theme
  setPref: (p: ThemePref) => void
}

const Ctx = createContext<ThemeState>({ pref: 'system', theme: 'dark', setPref: () => undefined })

/** Holds the preference; `fixed` pins a theme (Storybook's theme switch). */
export function ThemeProvider({ children, fixed }: { children: ReactNode; fixed?: Theme }) {
  const [pref, setState] = useState<ThemePref>(loadThemePref)
  const [systemLight, setSystemLight] = useState(() => query()?.matches ?? false)
  useEffect(() => {
    const q = query()
    if (!q) return
    const on = (e: MediaQueryListEvent) => setSystemLight(e.matches)
    q.addEventListener('change', on)
    return () => q.removeEventListener('change', on)
  }, [])
  const theme = fixed ?? resolveTheme(pref, systemLight)
  useEffect(() => applyTheme(theme), [theme])
  const setPref = useCallback((p: ThemePref) => {
    saveThemePref(p)
    setState(p)
  }, [])
  const value = useMemo(() => ({ pref, theme, setPref }), [pref, theme, setPref])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export const useTheme = (): ThemeState => useContext(Ctx)
