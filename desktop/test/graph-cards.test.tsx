import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import configErrorOpen from './fixtures/graph-open-config-error.json'
import inputsOpen from './fixtures/graph-open-with-inputs.json'
import mixedOpen from './fixtures/graph-open-mixed.json'
import { GraphInputsCard } from '../src/editor/GraphInputsCard'
import { GraphOutputsCard } from '../src/editor/GraphOutputsCard'
import { hintsOf } from '../src/editor/hints'
import { NeedsConnection } from '../src/editor/NeedsConnection'
import type { Definition } from '../src/editor/types'
import type { GraphDocument } from '../src/editor/useGraphDocument'
import { I18nProvider } from '../src/i18n/context'
import type { Diagnostic } from '../src/types'

const def = (o: unknown) => (o as { definition: Definition }).definition
const g = {} as GraphDocument
const render = (el: React.ReactNode, locale: 'en' | 'pl' = 'en') => renderToStaticMarkup(<I18nProvider locale={locale}>{el}</I18nProvider>)

describe('Graph inputs card', () => {
  it('is a labelled region with each input, its bindings and visible field labels', () => {
    const out = render(<GraphInputsCard g={g} def={def(inputsOpen)} readOnly={false} />)
    expect(out).toContain('role="region" aria-label="Graph inputs"')
    expect(out).toContain('data-name="topic"')
    expect(out).toContain('echo.inputs.topic')
    expect(out).toContain('aria-label="Type of graph input topic"')
    expect(out).toContain('aria-label="New input name"')
    expect(out).toContain('aria-label="Remove graph input topic"')
  })
  it('says when there are none, and offers no form while read-only', () => {
    const empty = { ...def(inputsOpen), inputs: {}, graph_input_bindings: [] }
    expect(render(<GraphInputsCard g={g} def={empty} readOnly={false} />)).toContain('graph-inputs-empty')
    expect(render(<GraphInputsCard g={g} def={def(inputsOpen)} readOnly={true} />)).not.toContain('add-graph-input"')
  })
  it('is localised', () => {
    expect(render(<GraphInputsCard g={g} def={def(inputsOpen)} readOnly={false} />, 'pl')).toContain('Wejścia grafu')
  })
})

describe('Graph outputs card', () => {
  it('shows an output on an optional port as written, with its note, and offers no required output to add', () => {
    const out = render(<GraphOutputsCard g={g} def={def(configErrorOpen)} readOnly={false} />)
    expect(out).toContain('data-name="result"')
    expect(out).toContain('work.outputs.stdout')
    expect(out).toContain('optional output, kept as written')
    expect(out).toContain('No node declares a required output')
    expect(out).not.toContain('data-testid="add-graph-output"')
  })
  it('lists only required outputs in the add form', () => {
    const d: Definition = { id: 'x', nodes: { n: { type: 'shell', outputs: { yes: { type: 'string', required: true }, no: { type: 'string' } } } }, edges: [], outputs: {} }
    const out = render(<GraphOutputsCard g={g} def={d} readOnly={false} />)
    expect(out).toContain('<option value="n.outputs.yes">')
    expect(out).not.toContain('n.outputs.no')
  })
})

describe('Needs a connection', () => {
  const d = def(mixedOpen)
  const hints = hintsOf((mixedOpen as unknown as { diagnostics: Diagnostic[] }).diagnostics, d)
  const out = render(<NeedsConnection g={g} def={d} hints={hints} readOnly={false} announce={() => undefined} />)
  it('shows each hint with the engine message, an Options button and, for an unbound input, Add graph input', () => {
    expect(out).toContain('data-testid="needs-connection"')
    expect(out.match(/data-testid="hint-options"/g)).toHaveLength(4)
    expect(out.match(/data-testid="hint-add-input"/g)).toHaveLength(2)
    expect(out).toContain('required input &quot;free&quot; on node &quot;b&quot; has no valid binding')
    expect(out).toContain('aria-label="Add graph input for b.inputs.free"')
  })
  it('applies nothing by itself: no candidate is rendered before Options is clicked', () => {
    expect(out).not.toContain('hint-option-')
  })
  it('says when nothing needs a connection', () => {
    expect(render(<NeedsConnection g={g} def={d} hints={[]} readOnly={false} announce={() => undefined} />)).toContain('Nothing needs a connection.')
  })
})
