import { describe, expect, it } from 'vitest'
import bindingsOpen from './fixtures/graph-open-bindings.json'
import listBindings from './fixtures/graph-edit-list-connections-bindings.json'
import mixedOpen from './fixtures/graph-open-mixed.json'
import demoOpen from './fixtures/graph-open-demo.json'
import { addOp, bindingRows, isGraphInput, providersOf, removeOp, replaceOps, sourceChoices, sourceType } from '../src/editor/bindings'
import { listConnections, verdictsOf } from '../src/editor/connections'
import { pruneSelection, selectionFor, type Selection } from '../src/editor/selection'
import type { Definition, Op, OpResult } from '../src/editor/types'
import type { ApplyResult } from '../src/editor/useGraphDocument'
import type { Diagnostic } from '../src/types'

const def = (o: unknown) => (o as { definition: Definition }).definition
const bindings = def(bindingsOpen)
const mixed = def(mixedOpen)
const demo = def(demoOpen)
const diagnosticsOf = (o: unknown) => (o as { diagnostics: Diagnostic[] }).diagnostics

describe('binding rows', () => {
  it('lists edges and graph input bindings in one list, ordered by target node, then input', () => {
    const rows = bindingRows(bindings, [])
    expect(rows.map((r) => `${r.kind} ${r.from} -> ${r.to}`)).toEqual(['edge a.outputs.stdout -> b.inputs.text', 'binding inputs.topic -> c.inputs.seed'])
  })

  it('orders several inputs of one node by input name, and equal targets by source', () => {
    const d: Definition = {
      id: 'x',
      inputs: { s: { type: 'string' } },
      nodes: { t: { type: 'shell', inputs: { b: { type: 'string' }, a: { type: 'string' } } }, p: { type: 'shell', outputs: { o: { type: 'string' } } } },
      edges: [
        { from: 'p.outputs.o', to: 't.inputs.b' },
        { from: 'p.outputs.o', to: 't.inputs.a' },
      ],
      graph_input_bindings: [{ from: 'inputs.s', to: 't.inputs.a' }],
      outputs: {},
    }
    expect(bindingRows(d, []).map((r) => `${r.from} -> ${r.to}`)).toEqual(['inputs.s -> t.inputs.a', 'p.outputs.o -> t.inputs.a', 'p.outputs.o -> t.inputs.b'])
  })

  it('is stable: the same definition gives the same rows', () => {
    expect(bindingRows(demo, [])).toEqual(bindingRows(structuredClone(demo), []))
  })

  it('shows the declared type of the source, for an output and for a graph input', () => {
    const rows = bindingRows(bindings, [])
    expect(rows.map((r) => r.type)).toEqual(['string', 'string'])
    expect(bindingRows(demo, []).find((r) => r.from === 'flaky.outputs.json')?.type).toBe('json')
    expect(sourceType(bindings, 'inputs.nope')).toBe('')
  })

  it('maps badges from the diagnostics own edge and binding indexes', () => {
    const rows = bindingRows(mixed, diagnosticsOf(mixedOpen))
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ errors: 1, warnings: 0 })
    const d = (location: Diagnostic['location'], severity = 'error'): Diagnostic => ({ severity, code: 'X', message: 'm', location })
    const found = bindingRows(bindings, [d({ edge_index: 0 }, 'warning'), d({ graph_input_binding_index: 0 }), d({ graph_input_binding_index: 0 }), d({ node_id: 'b' })])
    expect(found.map((r) => [r.errors, r.warnings])).toEqual([
      [0, 1],
      [2, 0],
    ])
  })

  it('has no row for a graph without bindings', () => {
    expect(bindingRows({ id: 'e', nodes: {}, edges: [], outputs: {} }, [])).toEqual([])
  })
})

describe('operations built from a choice', () => {
  it('adds an edge for an output and a graph input binding for an input', () => {
    expect(addOp('a.outputs.o', 'b.inputs.i')).toEqual({ op: 'add_edge', from: 'a.outputs.o', to: 'b.inputs.i' })
    expect(addOp('inputs.topic', 'b.inputs.i')).toEqual({ op: 'add_graph_input_binding', from: 'inputs.topic', to: 'b.inputs.i' })
    expect(isGraphInput('inputs.topic')).toBe(true)
    expect(isGraphInput('a.outputs.o')).toBe(false)
  })

  it('removes with the operation of its kind', () => {
    expect(removeOp({ kind: 'edge', from: 'a.outputs.o', to: 'b.inputs.i' })).toEqual({ op: 'remove_edge', from: 'a.outputs.o', to: 'b.inputs.i' })
    expect(removeOp({ kind: 'binding', from: 'inputs.t', to: 'b.inputs.i' })).toEqual({ op: 'remove_graph_input_binding', from: 'inputs.t', to: 'b.inputs.i' })
  })

  it('replaces an edge provider with the two-operation batch [remove old, add new]', () => {
    expect(replaceOps(bindings, 'c.outputs.stdout', 'b.inputs.text')).toEqual<Op[]>([
      { op: 'remove_edge', from: 'a.outputs.stdout', to: 'b.inputs.text' },
      { op: 'add_edge', from: 'c.outputs.stdout', to: 'b.inputs.text' },
    ])
  })

  it('replaces a graph input binding provider, and replaces with a graph input', () => {
    expect(replaceOps(bindings, 'a.outputs.stdout', 'c.inputs.seed')).toEqual<Op[]>([
      { op: 'remove_graph_input_binding', from: 'inputs.topic', to: 'c.inputs.seed' },
      { op: 'add_edge', from: 'a.outputs.stdout', to: 'c.inputs.seed' },
    ])
    expect(replaceOps(bindings, 'inputs.topic', 'b.inputs.text')).toEqual<Op[]>([
      { op: 'remove_edge', from: 'a.outputs.stdout', to: 'b.inputs.text' },
      { op: 'add_graph_input_binding', from: 'inputs.topic', to: 'b.inputs.text' },
    ])
  })

  it('has nothing to replace on an input nothing feeds', () => {
    expect(replaceOps(bindings, 'a.outputs.stdout', 'b.inputs.count')).toBeNull()
    expect(providersOf(bindings, 'b.inputs.count')).toEqual([])
  })

  it('offers every declared source, graph inputs first, then outputs by node and port', () => {
    expect(sourceChoices(bindings).map((c) => `${c.ref}:${c.type}`)).toEqual(['inputs.topic:string', 'a.outputs.stdout:string', 'c.outputs.stdout:string'])
  })
})

describe('the connection query', () => {
  const answer = (query: unknown): ((ops: Op[]) => Promise<ApplyResult>) => async (ops) => {
    expect(ops).toHaveLength(1)
    return { ok: true, results: [{ index: 0, op: 'list_connections', query } as OpResult] }
  }
  const options = (listBindings.body.results[0] as unknown as OpResult).query!.options!

  it('sends exactly one list_connections operation, whatever the size of the graph', async () => {
    const sent: Op[][] = []
    await listConnections(async (ops) => {
      sent.push(ops)
      return { ok: true, results: [{ index: 0, op: 'list_connections', query: { ok: true, options: [] } }] }
    }, { from: 'a.outputs.o' })
    expect(sent).toEqual([[{ op: 'list_connections', from: 'a.outputs.o' }]])
  })

  it('returns the engine options as received, including refused ones with their message', async () => {
    const a = await listConnections(answer({ ok: true, options }), { from: 'c.outputs.stdout' })
    expect(a).toEqual({ ok: true, options })
    expect(options.filter((o) => !o.ok).every((o) => !!o.message && !!o.code)).toBe(true)
  })

  it('treats an omitted list as no counterpart, not as a failure', async () => {
    expect(await listConnections(answer({ ok: true }), { to: 'b.inputs.i' })).toEqual({ ok: true, options: [] })
  })

  it('reports a refused query or batch instead of returning an empty list', async () => {
    expect(await listConnections(answer({ ok: false, message: 'node "x" does not exist' }), { from: 'x.outputs.o' })).toEqual({ ok: false, message: 'node "x" does not exist' })
    expect(await listConnections(async () => ({ ok: false, message: 'the engine could not be reached' }), { from: 'a.outputs.o' })).toEqual({ ok: false, message: 'the engine could not be reached' })
    expect(await listConnections(async () => ({ ok: true, results: [] }), { from: 'a.outputs.o' })).toMatchObject({ ok: false })
  })

  it('keeps the verdict per counterpart for the drag highlight', () => {
    const v = verdictsOf(options)
    expect(Object.keys(v)).toEqual(options.map((o) => o.ref))
    expect(v['b.inputs.text']).toMatchObject({ ok: false, code: expect.any(String), message: expect.stringContaining('multiple providers') })
  })
})

describe('the selection the panel shares with the canvas', () => {
  it('keeps what the definition still holds and drops the rest', () => {
    const edge: Selection = { kind: 'edge', from: 'a.outputs.stdout', to: 'b.inputs.text' }
    const binding: Selection = { kind: 'binding', from: 'inputs.topic', to: 'c.inputs.seed' }
    expect(pruneSelection(edge, bindings)).toBe(edge)
    expect(pruneSelection(binding, bindings)).toBe(binding)
    expect(pruneSelection({ kind: 'node', id: 'a' }, bindings)).toEqual({ kind: 'node', id: 'a' })
    expect(pruneSelection({ kind: 'edge', from: 'a.outputs.stdout', to: 'c.inputs.seed' }, bindings)).toBeNull()
    expect(pruneSelection({ kind: 'binding', from: 'inputs.topic', to: 'b.inputs.text' }, bindings)).toBeNull()
    expect(pruneSelection({ kind: 'node', id: 'gone' }, bindings)).toBeNull()
    expect(pruneSelection(edge, undefined)).toBeNull()
  })

  it('selects the element an issue names', () => {
    expect(selectionFor({ kind: 'edge', index: 0, from: 'a.outputs.o', to: 'b.inputs.i' })).toEqual({ kind: 'edge', from: 'a.outputs.o', to: 'b.inputs.i' })
    expect(selectionFor({ kind: 'binding', index: 0, from: 'inputs.t', to: 'b.inputs.i' })).toEqual({ kind: 'binding', from: 'inputs.t', to: 'b.inputs.i' })
    expect(selectionFor({ kind: 'port', node: 'b', side: 'inputs', port: 'i' })).toEqual({ kind: 'node', id: 'b' })
    expect(selectionFor({ kind: 'graph' })).toBeNull()
  })
})
