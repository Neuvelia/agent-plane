import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else yield p
  }
}
const src = new URL('../src', import.meta.url).pathname
const stories = [...walk(src)].filter((f) => f.endsWith('.stories.tsx'))

describe('stories', () => {
  it('exist for primitives and screens', () => {
    expect(stories.length).toBeGreaterThanOrEqual(4)
  })
  it('never build node/run state by hand: screens use recorded fixtures', () => {
    for (const f of stories) {
      const text = readFileSync(f, 'utf8')
      expect(text, f).not.toMatch(/:\s*(NodeView|RunView|Attempt)\b\s*=/)
      expect(text, f).not.toMatch(/\bas\s+(NodeView|RunView)\b/)
      expect(text, f).not.toMatch(/nodes:\s*\{[^}]*status:/)
    }
  })
})
