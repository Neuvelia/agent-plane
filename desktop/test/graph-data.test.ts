import { describe, expect, it } from 'vitest'
import inputsOpen from './fixtures/graph-open-with-inputs.json'
import mixedOpen from './fixtures/graph-open-mixed.json'
import pairOpen from './fixtures/graph-open-pair.json'
import configErrorOpen from './fixtures/graph-open-config-error.json'
import removeInput from './fixtures/graph-edit-remove-graph-input-with-inputs.json'
import { addInputFor, graphInputRows, outputChoices, outputOptions, outputRows, suggestInputName } from '../src/editor/graphData'
import { OPTIONS_SHOWN, acceptedOptions, hintsOf, visibleOptions } from '../src/editor/hints'
import { describeChange } from '../src/editor/inspector/model'
import type { ConnectionOption, Definition } from '../src/editor/types'
import type { Diagnostic } from '../src/types'

const def = (o: unknown) => (o as { definition: Definition }).definition
const diags = (o: unknown) => (o as { diagnostics: Diagnostic[] }).diagnostics
const withInputs = def(inputsOpen)
const mixed = def(mixedOpen)
const pair = def(pairOpen)

describe('graph input rows', () => {
  it('lists name, type and required with the bindings that start at the input', () => {
    expect(graphInputRows(withInputs)).toEqual([{ name: 'topic', type: 'string', required: true, bindings: [{ kind: 'binding', from: 'inputs.topic', to: 'echo.inputs.topic' }] }])
  })
  it('is empty for a graph without inputs and sorted by name otherwise', () => {
    expect(graphInputRows(pair)).toEqual([])
    const d: Definition = { ...pair, inputs: { b: { type: 'string' }, a: { type: 'number', required: false } } }
    expect(graphInputRows(d).map((r) => [r.name, r.required])).toEqual([['a', false], ['b', false]])
  })
})

describe('naming a graph input after a port', () => {
  it('uses the port name, then the lowest free numeric suffix', () => {
    expect(suggestInputName('free', [])).toBe('free')
    expect(suggestInputName('free', ['free'])).toBe('free_2')
    expect(suggestInputName('free', ['free', 'free_2', 'free_4'])).toBe('free_3')
  })
  it('is deterministic', () => {
    expect(suggestInputName('x', ['x'])).toBe(suggestInputName('x', ['x']))
  })
  it('builds one batch: declare a required input of the port type, then bind it', () => {
    expect(addInputFor(withInputs, 'echo.inputs.topic', 'string')).toEqual([
      { op: 'add_graph_input', id: 'topic_2', type: 'string', required: true },
      { op: 'add_graph_input_binding', from: 'inputs.topic_2', to: 'echo.inputs.topic' },
    ])
    expect(addInputFor(mixed, 'b.inputs.free', 'string')[0]).toMatchObject({ id: 'free' })
  })
})

describe('graph outputs', () => {
  const d: Definition = {
    id: 'o',
    nodes: { n: { type: 'shell', outputs: { need: { type: 'string', required: true }, opt: { type: 'string' }, also: { type: 'json', required: true } } } },
    edges: [],
    outputs: { kept: { from: 'n.outputs.opt' }, main: { from: 'n.outputs.need' } },
  }
  it('offers only required outputs', () => {
    expect(outputChoices(d)).toEqual(['n.outputs.also', 'n.outputs.need'])
    expect(outputChoices(def(pairOpen))).toEqual([])
  })
  it('shows an existing output on an optional port as written, but never offers it again', () => {
    expect(outputRows(d)).toEqual([
      { name: 'kept', from: 'n.outputs.opt', optionalSource: true },
      { name: 'main', from: 'n.outputs.need', optionalSource: false },
    ])
    expect(outputOptions(d, 'n.outputs.opt')).toEqual(['n.outputs.opt', 'n.outputs.also', 'n.outputs.need'])
    expect(outputOptions(d, 'n.outputs.need')).toEqual(['n.outputs.also', 'n.outputs.need'])
    // The add form has no current value: the optional port is not among its choices.
    expect(outputChoices(d)).not.toContain('n.outputs.opt')
  })
  it('keeps an output whose source cannot be found, without marking it optional', () => {
    expect(outputRows({ ...d, outputs: { x: { from: 'gone.outputs.o' } } })).toEqual([{ name: 'x', from: 'gone.outputs.o', optionalSource: false }])
  })
})

describe('hints derive from diagnostic codes and engine options only', () => {
  it('lists unused outputs and unbound required inputs of the last result, with the engine message', () => {
    const hints = hintsOf(diags(mixedOpen), mixed)
    expect(hints.map((h) => [h.kind, h.ref])).toEqual([
      ['unbound-input', 'b.inputs.free'],
      ['unbound-input', 'b.inputs.n'],
      ['unused-output', 'a.outputs.out'],
      ['unused-output', 'a.outputs.spare'],
    ])
    expect(hints[0]!.message).toContain('required input "free"')
  })
  it('ignores other diagnostics and a port that no longer exists', () => {
    const other: Diagnostic = { severity: 'error', code: 'E_NODE_CONFIG_INVALID', message: 'm', location: { node_id: 'a', path: 'nodes.a.config' } }
    const gone: Diagnostic = { severity: 'warning', code: 'W_DECLARED_OUTPUT_UNUSED', message: 'm', location: { node_id: 'zz', path: 'nodes.zz.outputs.o' } }
    expect(hintsOf([other, gone], mixed)).toEqual([])
    expect(hintsOf(diags(configErrorOpen), def(configErrorOpen))).toEqual([])
  })
  it('has no hint for a graph without those diagnostics', () => {
    expect(hintsOf([], mixed)).toEqual([])
  })

  const option = (i: number, ok: boolean, required = false): ConnectionOption => ({ ref: `n${i}.inputs.i`, type: 'string', required, ok })
  it('offers accepted candidates only, required ones first', () => {
    const all = [option(1, true), option(2, false), option(3, true, true), option(4, true)]
    expect(acceptedOptions(all).map((o) => o.ref)).toEqual(['n3.inputs.i', 'n1.inputs.i', 'n4.inputs.i'])
  })
  it('shows at most 20 first, then everything on request', () => {
    const many = Array.from({ length: 35 }, (_, i) => option(i, true))
    expect(OPTIONS_SHOWN).toBe(20)
    expect(visibleOptions(many, false)).toHaveLength(20)
    expect(visibleOptions(many, true)).toHaveLength(35)
    expect(visibleOptions(many.slice(0, 5), false)).toHaveLength(5)
  })
})

describe('removal preview', () => {
  it('lists the graph input bindings that go with the input, from the engine answer', () => {
    const after = (removeInput.body as unknown as { definition: Definition }).definition
    const bindings = describeChange(withInputs, after).filter((c) => c.what === 'binding')
    expect(bindings).toEqual([{ what: 'binding', how: 'removed', text: 'inputs.topic → echo.inputs.topic' }])
  })
})
