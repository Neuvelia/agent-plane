import { describe, expect, it } from 'vitest'
import { catalogues, format, loadLocale, pickLocale, saveLocale, translate } from '../src/i18n'

const params = (m: string) => [...m.matchAll(/\{(\w+)(?:,|\})/g)].map((x) => x[1]!).filter((n, i, a) => a.indexOf(n) === i).sort()

describe('catalogues', () => {
  const en = catalogues.en
  const pl = catalogues.pl
  it('en and pl have identical keys', () => {
    expect(Object.keys(pl).sort()).toEqual(Object.keys(en).sort())
  })
  it('every key has the same parameter names in both languages', () => {
    for (const key of Object.keys(en) as (keyof typeof en)[]) expect(params(pl[key]), key).toEqual(params(en[key]))
  })
  it('no message is empty', () => {
    for (const c of [en, pl]) for (const [k, v] of Object.entries(c)) expect(v.trim(), k).not.toBe('')
  })
})

describe('plurals', () => {
  const pluralPl = (n: number) => translate('pl', 'run.events', { count: n })
  it('Polish uses one / few / many', () => {
    expect(pluralPl(1)).toBe('1 zdarzenie')
    expect(pluralPl(2)).toBe('2 zdarzenia')
    expect(pluralPl(4)).toBe('4 zdarzenia')
    expect(pluralPl(5)).toBe('5 zdarzeń')
    expect(pluralPl(12)).toBe('12 zdarzeń')
    expect(pluralPl(14)).toBe('14 zdarzeń')
    expect(pluralPl(22)).toBe('22 zdarzenia')
  })
  it('English uses one / other', () => {
    expect(translate('en', 'run.events', { count: 1 })).toBe('1 event')
    expect(translate('en', 'run.events', { count: 5 })).toBe('5 events')
  })
})

describe('format', () => {
  it('substitutes parameters and leaves unknown ones visible', () => {
    expect(format('Retrying {n}/{max}', 'en', { n: 2, max: 3 })).toBe('Retrying 2/3')
    expect(format('Hello {who}', 'en')).toBe('Hello {who}')
  })
  it('never interprets parameter values', () => {
    expect(format('x {a}', 'en', { a: '{b} {n, plural, other {boom}}' })).toBe('x {b} {n, plural, other {boom}}')
  })
  it('falls back to the key for an unknown key', () => {
    // @ts-expect-error deliberately not a catalogue key
    expect(translate('pl', 'no.such.key')).toBe('no.such.key')
  })
})

describe('locale choice', () => {
  it('picks pl for Polish system locales and en for everything else', () => {
    expect(pickLocale('pl-PL')).toBe('pl')
    expect(pickLocale('pl')).toBe('pl')
    expect(pickLocale('de-DE')).toBe('en')
    expect(pickLocale(undefined)).toBe('en')
  })
  it('round-trips through storage', () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) }
    expect(loadLocale(storage)).toBeUndefined()
    saveLocale('pl', storage)
    expect(loadLocale(storage)).toBe('pl')
  })
  it('survives unavailable or hostile storage', () => {
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } }
    expect(loadLocale(broken)).toBeUndefined()
    expect(() => saveLocale('pl', broken)).not.toThrow()
    expect(loadLocale({ getItem: () => 'klingon' })).toBeUndefined()
  })
})
