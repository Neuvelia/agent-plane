import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { loadLocale, pickLocale, saveLocale, translate, type Locale, type MessageKey, type Params } from './index'

interface I18n {
  locale: Locale
  setLocale: (l: Locale) => void
  t: (key: MessageKey, params?: Params) => string
}

const Ctx = createContext<I18n | null>(null)

/** Initial language: saved choice, else the system locale (pl or en), else English. */
function initial(): Locale {
  return loadLocale() ?? pickLocale(typeof navigator === 'undefined' ? undefined : navigator.language)
}

export function I18nProvider({ children, locale: fixed }: { children: ReactNode; locale?: Locale }) {
  const [state, setState] = useState<Locale>(fixed ?? initial)
  const locale = fixed ?? state
  const setLocale = useCallback((l: Locale) => {
    saveLocale(l)
    setState(l)
  }, [])
  const value = useMemo<I18n>(() => ({ locale, setLocale, t: (key, params) => translate(locale, key, params) }), [locale, setLocale])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useI18n(): I18n {
  const v = useContext(Ctx)
  if (!v) throw new Error('useI18n must be used inside <I18nProvider>')
  return v
}
