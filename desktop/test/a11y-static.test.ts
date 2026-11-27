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
const files = [...walk(src)].filter((f) => /\.tsx$/.test(f) && !f.endsWith('.stories.tsx'))
const styles = readFileSync(join(src, 'styles.css'), 'utf8')

describe('interactive elements are native', () => {
  it('no div or span has a click or key handler, and nothing fakes a button or link role', () => {
    for (const f of files) {
      const text = readFileSync(f, 'utf8')
      expect(text, f).not.toMatch(/<(div|span|li|td|tr)\b[^>]*\bon(Click|KeyUp|MouseDown)\b/)
      // The one exception: a role="menu" container (F2.5c) may take onKeyDown, as a composite widget owns its arrow-key model.
      for (const m of text.matchAll(/<(div|span|li|td|tr)\b[^>]*\bonKeyDown\b[^>]*/g)) expect(m[0], f).toMatch(/\brole="menu"/)
      expect(text, f).not.toMatch(/role=["'](button|link|tab|switch|checkbox)["']/)
      // Menu items are native buttons that take a menu role; nothing else may.
      expect(text, f).not.toMatch(/<(?!button\b)\w+\b[^>]*role=["']menuitem/)
    }
  })
})

describe('focus and motion', () => {
  it('draws the 2px focus outline for every control and never removes it without a replacement', () => {
    expect(styles).toMatch(/:focus-visible\{outline:2px solid var\(--focus\);outline-offset:2px\}/)
    // The only outline:none is the programmatic focus target (main), which is not a control.
    const removed = styles.split('\n').filter((l) => /outline:\s*none/.test(l))
    expect(removed.every((l) => l.startsWith('main:focus'))).toBe(true)
  })
  it('removes all transitions and animations under prefers-reduced-motion', () => {
    expect(styles).toMatch(/@media \(prefers-reduced-motion:reduce\)\{\*\{transition:none!important;animation:none!important\}\}/)
  })
  it('also removes transitions and animations of pseudo-elements, which `*` does not match', () => {
    expect(styles).toMatch(/@media \(prefers-reduced-motion:reduce\)\{\*::before,\*::after\{transition:none!important;animation:none!important\}\}/)
  })
  it('has no scroll-behavior smooth that the reduced-motion rule would not cover', () => {
    expect(styles).not.toMatch(/scroll-behavior:\s*smooth/)
  })
})
