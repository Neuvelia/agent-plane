import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// WCAG contrast of the colour pairs the app actually draws, computed from the
// shipped tokens.css (+ overrides.css), for every theme. Text must reach 4.5:1;
// focus rings and selection outlines 3:1. Translucent colours are composited over
// what is behind. The light theme redefines colours in `:root[data-theme="light"]`.
const css = ['tokens.css', 'overrides.css'].map((f) => readFileSync(new URL(`../src/design/${f}`, import.meta.url), 'utf8')).join('\n').replace(/\/\*[\s\S]*?\*\//g, '')
const blocks = [...css.matchAll(/(:root(?:\[data-theme="(\w+)"\])?)\s*\{([^}]*)\}/g)]
const declared = (body: string) => [...body.matchAll(/--([\w-]+):\s*([^;}]+?)\s*(?:;|$)/g)].map((m) => [m[1]!, m[2]!] as const)
function tokensOf(theme: 'dark' | 'light'): Map<string, string> {
  const map = new Map<string, string>()
  for (const b of blocks) if (!b[2]) for (const [k, v] of declared(b[3]!)) map.set(k, v)
  if (theme === 'light') for (const b of blocks) if (b[2] === 'light') for (const [k, v] of declared(b[3]!)) map.set(k, v)
  return map
}

type RGBA = [number, number, number, number]
const over = (top: RGBA, under: RGBA): RGBA => {
  const a = top[3] + under[3] * (1 - top[3])
  const ch = (i: number) => (top[i]! * top[3] + under[i]! * under[3] * (1 - top[3])) / (a || 1)
  return [ch(0), ch(1), ch(2), a]
}
const lum = ([r, g, b]: RGBA) => {
  const f = (c: number) => ((c /= 255) <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}
const ratio = (a: RGBA, b: RGBA) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi! + 0.05) / (lo! + 0.05)
}

for (const theme of ['dark', 'light'] as const) {
const token = tokensOf(theme)
function parse(name: string): RGBA {
  const v = token.get(name)
  if (!v) throw new Error(`no token --${name}`)
  const hex = /^#([0-9a-f]{3,8})$/i.exec(v)
  if (hex) {
    let h = hex[1]!
    if (h.length === 3) h = [...h].map((c) => c + c).join('')
    return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16), h.length === 8 ? parseInt(h.slice(6, 8), 16) / 255 : 1]
  }
  const rgba = /^rgba?\(([^)]+)\)$/.exec(v)
  if (rgba) {
    const [r, g, b, a] = rgba[1]!.split(',').map((x) => Number(x.trim()))
    return [r!, g!, b!, a ?? 1]
  }
  throw new Error(`cannot parse --${name}: ${v}`)
}
/** Layers listed bottom to top, composited. */
const stack = (...names: string[]): RGBA => names.map(parse).reduce((under, layer) => over(layer, under))

const card = stack('bg', 'card-mid')
const grounds: Record<string, RGBA> = {
  bg: parse('bg'),
  surface: stack('bg', 'surface'),
  inset: stack('bg', 'inset'),
  raised: stack('bg', 'raised'),
  'raised-hover': stack('bg', 'raised-hover'),
  'card-top': stack('bg', 'card-top'),
  card,
  'code-bg': stack('bg', 'code-bg'),
  'fill-8 pill': stack('bg', 'card-mid', 'fill-8'),
  'fill-6 tag': stack('bg', 'card-mid', 'fill-6'),
  'fill-4 hover': stack('bg', 'card-mid', 'fill-4'),
  'warn-wash': stack('bg', 'card-mid', 'warn-wash'),
  'inset fill-8 (editor canvas chip)': stack('bg', 'inset', 'fill-8'),
}
const text = (fg: string, on: Record<string, RGBA>) => Object.entries(on).map(([n, bg]) => ({ pair: `${fg} on ${n}`, value: ratio(over(parse(fg), bg), bg) }))

describe(`${theme} theme`, () => {
describe('text contrast reaches 4.5:1', () => {
  const cases = [
    ...text('fg', grounds),
    ...text('muted', grounds),
    ...text('brand', { bg: grounds.bg!, card, 'brand-soft': stack('bg', 'card-mid', 'brand-soft') }),
    ...text('ok', { bg: grounds.bg!, card, 'ok-soft': stack('bg', 'card-mid', 'ok-soft') }),
    ...text('warn', { bg: grounds.bg!, card, 'warn-soft': stack('bg', 'card-mid', 'warn-soft'), 'warn-wash': grounds['warn-wash']! }),
    ...text('bad-text', { bg: grounds.bg!, card, 'bad-soft': stack('bg', 'card-mid', 'bad-soft'), 'bad-soft on warn-wash': stack('bg', 'card-mid', 'warn-wash', 'bad-soft') }),
    ...text('on-silver', { 'silver-top': parse('silver-top'), 'silver-bottom': parse('silver-bottom') }),
    ...text('fg', { 'button-top': parse('button-top'), 'button-bottom': parse('button-bottom') }),
    ...text('brand-text', { bg: grounds.bg!, inset: grounds.inset!, 'brand-soft': stack('bg', 'brand-soft') }),
    ...text('primary-fg', { 'primary-bg': parse('primary-bg'), 'primary-hover': parse('primary-hover') }),
    ...['code-key', 'code-string', 'code-number', 'code-var'].flatMap((c) => text(c, { 'code-bg': grounds['code-bg']! })),
  ]
  for (const c of cases) it(c.pair, () => expect(c.value).toBeGreaterThanOrEqual(4.5))
})

describe('non-text contrast reaches 3:1', () => {
  it('keyboard focus ring on the page and on raised surfaces', () => {
    for (const bg of [grounds.bg!, grounds.raised!, grounds.card!]) expect(ratio(over(parse('focus'), bg), bg)).toBeGreaterThanOrEqual(3)
  })
  it('selected node ring on the surfaces it sits on', () => {
    expect(ratio(parse('brand'), grounds.raised!)).toBeGreaterThanOrEqual(3)
    expect(ratio(parse('brand'), grounds.inset!)).toBeGreaterThanOrEqual(3)
  })
  it('status dots on the card', () => {
    for (const c of ['ok', 'warn', 'brand', 'bad']) expect(ratio(parse(c), card), c).toBeGreaterThanOrEqual(3)
  })
  it('the flat primary button stands out from the ground', () => {
    expect(ratio(parse('primary-bg'), grounds.bg!)).toBeGreaterThanOrEqual(3)
  })
})

})
}

describe('the faint colour is never body text', () => {
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  it('muted-faint appears only for the decorative breadcrumb separator', () => {
    const uses = styles.split('\n').filter((l) => l.includes('var(--muted-faint)'))
    expect(uses).toHaveLength(1)
    expect(uses[0]).toContain('.crumb+.crumb::before')
  })
})
