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

describe('Inbox stories (F2.5b)', () => {
  // The smoke renders each required story in EN and PL, waits for its play function (which
  // asserts the state it drives: decided rows, pinning, focus) and fails on a play error or axe.
  it('the story smoke requires every Inbox and Sidebar state [F2.5b-AC3]', () => {
    const smoke = readFileSync(fileURLToPath(new URL('../scripts/stories-smoke.cjs', import.meta.url)), 'utf8')
    const required = [
      'needs-attention', 'failed-selected', 'retries-used-up', 'interrupted-selected', 'running-selected', 'filter-stopped', 'filter-empty',
      'decided', 'decided-here', 'run-not-found', 'decision-error', 'connection-lost', 'stays-on-shown-item', 'keyboard-focus',
      'empty', 'no-runs', 'loading', 'error',
    ].map((s) => `screens--inbox-${s}`)
    for (const id of [...required, 'ui-shell--sidebar-recent-runs', 'ui-shell--sidebar-recent-unavailable', 'ui-shell--sidebar-no-runs']) expect(smoke, id).toContain(`'${id}'`)
    expect(smoke).toMatch(/playFunctionThrewException/)
    expect(smoke).toMatch(/currentRender\?\.phase/)
  })
})

