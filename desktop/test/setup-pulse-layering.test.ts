import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const css = readFileSync(new URL('../src/setup.css', import.meta.url), 'utf8')

function rule(selector: string): string {
  const line = css.split('\n').find((l) => l.startsWith(`${selector}{`))
  if (!line) throw new Error(`missing rule ${selector}`)
  return line
}

describe('Setup pulse layering', () => {
  it('[F2.11] draws the welcome pulse below the node cards', () => {
    const z = (r: string) => Number(/z-index:(\d+)/.exec(r)?.[1])
    expect(z(rule('.su-pulse'))).toBeLessThan(z(rule('.su-node')))
  })

  it('[F2.11] lifts the first-graph step icons above the pulse on an opaque background', () => {
    const mini = rule('.su-path .su-mini')
    expect(mini).toContain('position:relative')
    expect(mini).toContain('z-index:1')
    expect(mini).toMatch(/,var\(--bg\)/)
  })
})
