import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import nodeTypes from './fixtures/node-types.json'
import { I18nProvider } from '../src/i18n/context'
import type { DocState } from '../src/editor/doc'
import { Inspector } from '../src/editor/inspector/Inspector'
import type { Definition, NodeTypeDescriptor, PortDef } from '../src/editor/types'
import type { GraphDocument } from '../src/editor/useGraphDocument'

// F2.2 Human node criterion, driven through the real Inspector component over the
// served descriptors (fixtures/node-types.json, pinned to the catalogue by
// cmd/agentplane/catalogue_test.go): a human node has no loop section and no
// output editing, and the context_inputs picker offers only declared string inputs
// and stops at the descriptor's max_items.
const cat = nodeTypes as unknown as Record<string, NodeTypeDescriptor>
const max = cat.human!.config.find((f) => f.key === 'context_inputs')!.max_items!

const str = (n: number): Record<string, PortDef> => Object.fromEntries(Array.from({ length: n }, (_, i) => [`s${i}`, { type: 'string', required: true }]))

function render(inputs: Record<string, PortDef>, contextInputs: string[], locale: 'en' | 'pl' = 'en') {
  const def: Definition = {
    id: 'g',
    nodes: {
      approve: { type: 'human', inputs, outputs: { decision: { type: 'string', required: true } }, config: { prompt: 'Ship it?', context_inputs: contextInputs } },
      other: { type: 'shell', config: { command: '/usr/bin/true' } },
    },
    edges: [],
    outputs: {},
  }
  const doc = { analysis: { status: 'ready', diagnostics: [], stale: false, definition: def, nodeTypes: cat, nodeInfo: { approve: { known_type: true, advanced: [], independent_of: ['other'] } } } } as unknown as DocState
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <Inspector g={{} as GraphDocument} doc={doc} nodeId="approve" readOnly={false} onRenamed={() => undefined} />
    </I18nProvider>,
  )
}

const pickerOptions = (html: string) => {
  const m = /<select[^>]*data-testid="list-context_inputs-add"[^>]*>(.*?)<\/select>/.exec(html)
  return m ? [...m[1]!.matchAll(/<option value="([^"]*)"/g)].map((o) => o[1]!).filter(Boolean) : undefined
}

// The markup of one section, from its heading's section tag to the closing tag.
const section = (html: string, testId: string) => new RegExp(`<section[^>]*data-testid="${testId}"[^>]*>.*?</section>`).exec(html)?.[0]
const outputs = (html: string) => /<div class="ins-ports" data-testid="ports-outputs">.*?<\/section>/.exec(html)![0]

describe.each(['en', 'pl'] as const)('human node inspector (%s)', (locale) => {
  const html = render({ summary: { type: 'string', required: true } }, ['summary'], locale)

  it('offers no loop', () => {
    expect(html).toContain('data-testid="inspector-node"')
    expect(html).not.toContain('data-testid="section-loop"')
    expect(html).not.toContain('data-testid="loop-enable"')
    expect(html).not.toContain('data-testid="loop-off"')
    expect(html).not.toContain('data-testid="loop-on"')
  })

  it('offers no output editing: locked, every control disabled, nothing to add', () => {
    const out = outputs(html)
    expect(out).toContain('data-testid="outputs-locked"')
    expect(out).not.toContain('data-testid="add-port-outputs"')
    const controls = [...out.matchAll(/<(input|select|button)\b[^>]*>/g)].map((m) => m[0])
    expect(controls.length).toBeGreaterThan(0)
    for (const c of controls) expect(c, c).toMatch(/\bdisabled\b/)
    expect(out).toContain('decision')
  })

  it('shows only the descriptor fields prompt, context_inputs and on_reject under Execution', () => {
    const keys = [...section(html, 'section-execution')!.matchAll(/data-field-key="([^"]*)"/g)].map((m) => m[1])
    expect(keys).toEqual(['prompt', 'context_inputs', 'on_reject'])
  })
})

describe('context_inputs picker', () => {
  it('lists only declared string inputs that are not chosen yet', () => {
    const inputs: Record<string, PortDef> = {
      summary: { type: 'string', required: true },
      report: { type: 'string' },
      count: { type: 'number' },
      blob: { type: 'json' },
      flag: { type: 'boolean' },
    }
    const options = pickerOptions(render(inputs, ['summary']))
    expect(options).toEqual(['report'])
    expect(options).not.toContain('ghost')
  })

  it('an undeclared name already in the file is shown but not offered again', () => {
    const html = render({ summary: { type: 'string', required: true } }, ['ghost'])
    expect(pickerOptions(html)).toEqual(['summary'])
    expect(html).toContain('value="ghost"')
  })

  it('has no picker when no declared string input is left', () => {
    expect(pickerOptions(render({ summary: { type: 'string' } }, ['summary']))).toBeUndefined()
    expect(pickerOptions(render({ n: { type: 'number' } }, []))).toBeUndefined()
  })

  it(`stops at ${max} entries`, () => {
    const below = render(str(max + 2), Array.from({ length: max - 1 }, (_, i) => `s${i}`))
    expect(pickerOptions(below)).toEqual([`s${max - 1}`, `s${max}`, `s${max + 1}`])
    const full = render(str(max + 2), Array.from({ length: max }, (_, i) => `s${i}`))
    expect(pickerOptions(full)).toBeUndefined()
    expect(full).toContain(`At most ${max} entries.`)
  })
})
