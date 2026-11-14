import { en, type MessageKey } from './en'
import { pl } from './pl'

export type Locale = 'en' | 'pl'
export type { MessageKey }
export const LOCALES: Locale[] = ['en', 'pl']
export const LOCALE_LABEL: Record<Locale, string> = { en: 'EN', pl: 'PL' }

const catalogues: Record<Locale, Record<MessageKey, string>> = { en, pl }
const STORAGE_KEY = 'agentplane.locale'

export type Params = Record<string, string | number>

/** Picks a language from a system locale tag such as "pl-PL"; anything else is English. */
export function pickLocale(tag: string | undefined | null): Locale {
  const base = (tag ?? '').toLowerCase().split(/[-_]/)[0]
  return base === 'pl' ? 'pl' : 'en'
}

/** Reads the saved choice. A missing or throwing localStorage yields undefined. */
export function loadLocale(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): Locale | undefined {
  try {
    const v = storage?.getItem(STORAGE_KEY)
    return v === 'en' || v === 'pl' ? v : undefined
  } catch {
    return undefined
  }
}

export function saveLocale(locale: Locale, storage: Pick<Storage, 'setItem'> | undefined = safeStorage()): void {
  try {
    storage?.setItem(STORAGE_KEY, locale)
  } catch {
    // Unavailable storage: the choice lasts for the session only.
  }
}

function safeStorage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage
  } catch {
    return undefined
  }
}

// Messages are "text {param}" with one optional plural form:
//   {name, plural, one {# thing} few {...} many {...} other {...}}
// '#' is the count. Nothing else is interpreted, and a value is never parsed.
const PLURAL = /\{(\w+), plural,((?:\s*\w+ \{[^{}]*\})+)\s*\}/g
const FORM = /(\w+) \{([^{}]*)\}/g

export function format(message: string, locale: Locale, params: Params = {}): string {
  const withPlurals = message.replace(PLURAL, (_, name: string, forms: string) => {
    const count = Number(params[name] ?? 0)
    const table: Record<string, string> = {}
    for (const m of forms.matchAll(FORM)) table[m[1]!] = m[2]!
    const category = new Intl.PluralRules(locale).select(count)
    const text = table[category] ?? table.other ?? ''
    return text.replace(/#/g, new Intl.NumberFormat(locale).format(count))
  })
  return withPlurals.replace(/\{(\w+)\}/g, (whole, name: string) => (name in params ? String(params[name]) : whole))
}

/** Translates a key. A key missing from the locale falls back to English, then to the key. */
export function translate(locale: Locale, key: MessageKey, params?: Params): string {
  const message = catalogues[locale][key] ?? en[key] ?? key
  return format(message, locale, params)
}

export function formatDateTime(locale: Locale, iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'medium' }).format(d)
}

export { catalogues }
