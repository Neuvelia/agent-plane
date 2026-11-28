import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// The colour tokens the graph editor v2 adds (F2.5f, ADR-0050): a value for every
// theme, and text pairs that reach 4.5:1 in both.

const json = JSON.parse(readFileSync(new URL('../src/design/tokens.json', import.meta.url), 'utf8'))
const NEW = ['line', 'brand-text', 'primary-bg', 'primary-fg', 'primary-hover', 'ring', 'elevation', 'elevation-menu', 'scrim']
const value = (name: string, theme: string): string => json.color.tokens.find((t: { name: string }) => t.name === name).value[theme]

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))
const lum = (hex: string) => {
  const [r, g, b] = rgb(hex).map((c) => ((c /= 255) <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!
}
const ratio = (a: string, b: string) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi! + 0.05) / (lo! + 0.05)
}

describe('graph editor v2 tokens', () => {
  it('every new token has a dark and a light value [F2.5f-AC4]', () => {
    for (const n of NEW) for (const theme of ['dark', 'light']) expect(value(n, theme), `${n}/${theme}`).toBeTruthy()
  })
  it('primary text and brand text reach 4.5:1 in both themes [F2.5f-AC4]', () => {
    for (const theme of ['dark', 'light']) {
      expect(ratio(value('primary-fg', theme), value('primary-bg', theme)), theme).toBeGreaterThanOrEqual(4.5)
      expect(ratio(value('primary-fg', theme), value('primary-hover', theme)), theme).toBeGreaterThanOrEqual(4.5)
      expect(ratio(value('brand-text', theme), value('bg', theme)), theme).toBeGreaterThanOrEqual(4.5)
      // The flat primary stands out from the ground.
      expect(ratio(value('primary-bg', theme), value('bg', theme)), theme).toBeGreaterThanOrEqual(3)
    }
  })
})
