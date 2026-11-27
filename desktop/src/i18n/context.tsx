import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { clearLocale, loadLocale, pickLocale, saveLocale, translate, type Locale, type MessageKey, type Params } from './index'

/** A language the person picked, or "system": follow the operating system's language. */
export type LocaleChoice = 'system' | Locale

interface I18n {
  locale: Locale
  /** What the person chose; "system" when nothing is saved. */
  choice: LocaleChoice
  setChoice: (c: LocaleChoice) => void
  t: (key: MessageKey, params?: Params) => string
}

const Ctx = createContext<I18n | null>(null)

function systemLocale(): Locale {
  return pickLocale(typeof navigator === 'undefined' ? undefined : navigator.language)
}

/** Initial choice: the saved language, else the system's (pl or en), else English. */
function initial(): LocaleChoice {
  return loadLocale() ?? 'system'
}

export function I18nProvider({ children, locale: fixed }: { children: ReactNode; locale?: Locale }) {
  const [state, setState] = useState<LocaleChoice>(initial)
  const locale = fixed ?? (state === 'system' ? systemLocale() : state)
  const setChoice = useCallback((c: LocaleChoice) => {
    if (c === 'system') clearLocale()
    else saveLocale(c)
    setState(c)
  }, [])
  const choice: LocaleChoice = fixed ?? state
  const value = useMemo<I18n>(() => ({ locale, choice, setChoice, t: (key, params) => translate(locale, key, params) }), [locale, choice, setChoice])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useI18n(): I18n {
  const v = useContext(Ctx)
  if (!v) throw new Error('useI18n must be used inside <I18nProvider>')
  return v
}
