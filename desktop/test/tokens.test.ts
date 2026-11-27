import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { buildTokensCss } from '../scripts/tokens-css.mjs'

const dir = new URL('../src/design/', import.meta.url)
const json = JSON.parse(readFileSync(new URL('tokens.json', dir), 'utf8'))
const css = readFileSync(new URL('tokens.css', dir), 'utf8')

describe('design tokens', () => {
  it('tokens.css is exactly what tokens.json generates', () => {
    expect(css).toBe(buildTokensCss(json))
  })

  it('every colour, spacing, radius, size and shadow token exists with the same value', () => {
    const base = css.slice(0, css.indexOf('}'))
    const declared = new Map([...base.matchAll(/^\s*--([\w-]+): (.+);$/gm)].map((m) => [m[1]!, m[2]!]))
    const theme = json.color.themes[0].id
    for (const t of json.color.tokens) expect(declared.get(t.name), t.name).toBe(t.value[theme])
    for (const fam of ['spacing', 'radius', 'size', 'shadow']) {
      for (const t of json[fam].tokens) expect(declared.get(t.name), t.name).toBe(t.value)
    }
  })

  it('every colour token has a light value, declared under [data-theme="light"] [F2.5b-AC4]', () => {
    const at = css.indexOf(':root[data-theme="light"]')
    expect(at).toBeGreaterThan(0)
    const light = new Map([...css.slice(at).matchAll(/^\s*--([\w-]+): (.+);$/gm)].map((m) => [m[1]!, m[2]!]))
    for (const t of json.color.tokens) {
      expect(t.value.light, t.name).toBeTruthy()
      expect(light.get(t.name), t.name).toBe(t.value.light)
    }
  })

  it('declares both font families and the text styles the app uses', () => {
    expect(css).toContain('--font-sans:')
    expect(css).toContain('--font-mono:')
    for (const s of ['body', 'label', 'card-title', 'stat', 'code']) expect(css).toContain(`--type-${s}-size:`)
  })
})
