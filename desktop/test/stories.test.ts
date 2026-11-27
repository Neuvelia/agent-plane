import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else yield p
  }
}
const src = fileURLToPath(new URL('../src', import.meta.url))
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

describe('the browser story smoke (scripts/stories-smoke.cjs)', () => {
  const smoke = readFileSync(fileURLToPath(new URL('../scripts/stories-smoke.cjs', import.meta.url)), 'utf8')
  it('renders every story in English and Polish dark and English light, and fails a broken play function [F2.5b-AC5]', () => {
    expect(smoke).toContain("[['en', 'dark'], ['pl', 'dark'], ['en', 'light']]")
    expect(smoke).toContain('globals=locale:${locale};theme:${theme}')
    expect(smoke).toContain('__STORYBOOK_PREVIEW__?.currentRender?.phase')
    expect(smoke).toContain('fail(`play function ${phase}`)')
  })
  it('requires the graph editor v2 artboard stories', () => {
    for (const id of ['design-step-cards-light', 'design-step-cards-dark', 'design-error-blocks-run', 'design-graph-menu', 'design-step-menu', 'design-empty-graph']) expect(smoke).toContain(`'screens-editor--${id}'`)
  })
})
