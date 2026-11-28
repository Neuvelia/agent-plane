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

const NON_INTERACTIVE = new Set(['div', 'span', 'li', 'td', 'tr'])
const FORBIDDEN_HANDLERS = new Set(['onClick', 'onKeyUp', 'onMouseDown'])
const FAKED_ROLES = new Set(['button', 'link', 'tab', 'switch', 'checkbox'])

interface OpeningTag {
  name: string
  attrs: Map<string, string | undefined>
  line: number
}

/** Index just past a quoted string or template literal starting at i (skipping escapes). */
function skipQuoted(text: string, i: number): number {
  const quote = text[i]
  for (let j = i + 1; j < text.length; j++) {
    if (text[j] === '\\') j++
    else if (text[j] === quote) return j + 1
  }
  return text.length
}

/** Index just past the balanced {...} expression starting at i; braces inside strings do not count. */
function skipExpression(text: string, i: number): number {
  let depth = 0
  for (let j = i; j < text.length; j++) {
    const c = text[j]
    if (c === '"' || c === "'" || c === '`') j = skipQuoted(text, j) - 1
    else if (c === '{') depth++
    else if (c === '}' && --depth === 0) return j + 1
  }
  return text.length
}

/** Reads the attributes of the opening tag whose name ends at i; the closing `>` of an arrow `=>` inside {...} is skipped as part of the expression. */
function readAttributes(text: string, start: number): Map<string, string | undefined> {
  const attrs = new Map<string, string | undefined>()
  let i = start
  while (i < text.length && text[i] !== '>') {
    const name = /^[A-Za-z_][\w:-]*/.exec(text.slice(i))
    if (!name) {
      i = text[i] === '{' ? skipExpression(text, i) : i + 1 // spread props and `/`
      continue
    }
    i += name[0].length
    let value: string | undefined
    if (text[i] === '=') {
      i++
      const from = i
      i = text[i] === '{' ? skipExpression(text, i) : skipQuoted(text, i)
      value = /^\{?\s*(["'])(.*)\1\s*\}?$/s.exec(text.slice(from, i))?.[2]
    }
    attrs.set(name[0], value)
  }
  return attrs
}

/** Finds JSX opening tags. A `<` after an identifier, `)` or `]` is a comparison or a generic, not a tag. */
function openingTags(text: string): OpeningTag[] {
  const tags: OpeningTag[] = []
  for (const m of text.matchAll(/(?<![\w)\]])<([A-Za-z][\w.]*)(?=[\s/>])/g)) {
    const start = m.index + m[0].length
    tags.push({ name: m[1]!, attrs: readAttributes(text, start), line: text.slice(0, m.index).split('\n').length })
  }
  return tags
}

/** Lists every a11y rule broken by a JSX opening tag, reading whole attribute lists rather than stopping at the first `>`. */
export function a11yViolations(fileName: string, text: string): string[] {
  const found: string[] = []
  for (const { name, attrs, line } of openingTags(text)) {
    const role = attrs.get('role')
    const where = `${fileName}:${line} <${name}>`
    if (NON_INTERACTIVE.has(name)) {
      // The other exception: a role="option" (F2.5d palette) is operated from the combobox input through aria-activedescendant, so the pointer handlers are the mouse path to the same action.
      if (role !== 'option') for (const h of FORBIDDEN_HANDLERS) if (attrs.has(h)) found.push(`${where} has ${h}`)
      // The one exception: a role="menu" container (F2.5c) may take onKeyDown, as a composite widget owns its arrow-key model.
      if (attrs.has('onKeyDown') && role !== 'menu') found.push(`${where} has onKeyDown without role="menu"`)
    }
    if (role && FAKED_ROLES.has(role)) found.push(`${where} fakes role="${role}"`)
    // Menu items are native buttons that take a menu role; nothing else may.
    if (role?.startsWith('menuitem') && name !== 'button') found.push(`${where} has role="${role}" on a non-button`)
  }
  return found
}

describe('interactive elements are native', () => {
  it('no div or span has a click or key handler, and nothing fakes a button or link role', () => {
    for (const f of files) expect(a11yViolations(f, readFileSync(f, 'utf8')), f).toEqual([])
  })
  it('catches a handler placed after an arrow-function prop', () => {
    const arrowFirst = '<span onMouseEnter={() => go()} onClick={go}>x</span>'
    expect(a11yViolations('fixture.tsx', arrowFirst)).toEqual(['fixture.tsx:1 <span> has onClick'])
    const keyAfterArrow = '<div onFocus={() => go()} onKeyDown={go}>x</div>'
    expect(a11yViolations('fixture.tsx', keyAfterArrow)).toHaveLength(1)
    const menu = '<div role="menu" onMouseEnter={() => go()} onKeyDown={go}>x</div>'
    expect(a11yViolations('fixture.tsx', menu)).toEqual([])
    const option = '<div role="option" onMouseMove={() => hover()} onClick={() => choose()}>x</div>'
    expect(a11yViolations('fixture.tsx', option)).toEqual([])
  })
  it('flags faked roles and menu items outside a native button', () => {
    expect(a11yViolations('f.tsx', '<a role="button">x</a>')).toHaveLength(1)
    expect(a11yViolations('f.tsx', "<div role={'menuitem'}>x</div>")).toHaveLength(1)
    expect(a11yViolations('f.tsx', '<button role="menuitem">x</button>')).toEqual([])
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
  it('has no scroll-behavior smooth that the reduced-motion rule would not cover', () => {
    expect(styles).not.toMatch(/scroll-behavior:\s*smooth/)
  })
})
