import { describe, expect, it } from 'vitest'
import { badgesOf, filterIssues, firstError, issueCounts, locate, parseIssue, toIssues } from '../src/editor/issues'
import type { Definition } from '../src/editor/types'
import type { Diagnostic } from '../src/types'

const def: Definition = {
  id: 'g',
  nodes: {
    a: { type: 'shell', outputs: { out: { type: 'string' } } },
    b: { type: 'shell', inputs: { in: { type: 'string' } } },
  },
  edges: [{ from: 'a.outputs.out', to: 'b.inputs.in' }],
  graph_input_bindings: [{ from: 'inputs.t', to: 'b.inputs.in' }],
  outputs: {},
}
const d = (severity: string, code: string, location: Diagnostic['location'], message = code): Diagnostic => ({ severity, code, message, location })

describe('locate', () => {
  it('finds nodes, ports, edges and bindings, and falls back to the graph [F2.4-AC2]', () => {
    expect(locate(d('error', 'X', { node_id: 'a' }), def).target).toEqual({ kind: 'node', id: 'a' })
    expect(locate(d('warning', 'X', { node_id: 'a', path: 'nodes.a.outputs.out' }), def).target).toEqual({ kind: 'port', node: 'a', side: 'outputs', port: 'out' })
    expect(locate(d('error', 'X', { edge_index: 0, path: 'edges[0]' }), def).target).toEqual({ kind: 'edge', index: 0, from: 'a.outputs.out', to: 'b.inputs.in' })
    expect(locate(d('error', 'X', { graph_input_binding_index: 0 }), def).target).toMatchObject({ kind: 'binding', index: 0 })
    expect(locate(d('error', 'X', { path: 'id' }), def).target).toEqual({ kind: 'graph' })
  })
  it('an element that no longer exists is shown at graph level', () => {
    expect(locate(d('error', 'X', { node_id: 'gone' }), def).target).toEqual({ kind: 'graph' })
    expect(locate(d('error', 'X', { edge_index: 7 }), def).target).toEqual({ kind: 'graph' })
    expect(locate(d('error', 'X', { node_id: 'a', path: 'nodes.a.outputs.ghost' }), def).target).toEqual({ kind: 'node', id: 'a' })
    expect(locate(d('error', 'X', { node_id: 'a' }), undefined).target).toEqual({ kind: 'graph' })
  })
})

describe('ordering and filtering', () => {
  const diags = [d('warning', 'W2', { node_id: 'b' }), d('error', 'E2', { node_id: 'b' }), d('warning', 'W1', { node_id: 'a' }), d('error', 'E1', { node_id: 'b' }), d('error', 'E0', { node_id: 'a' })]
  it('errors first, then warnings, then element and code, deterministically', () => {
    const codes = toIssues(diags, def).map((i) => i.code)
    expect(codes).toEqual(['E0', 'E1', 'E2', 'W1', 'W2'])
    expect(toIssues([...diags].reverse(), def).map((i) => i.code)).toEqual(codes)
  })
  it('filters and counts', () => {
    const issues = toIssues(diags, def)
    expect(issueCounts(issues)).toEqual({ all: 5, error: 3, warning: 2 })
    expect(filterIssues(issues, 'error').every((i) => i.severity === 'error')).toBe(true)
    expect(filterIssues(issues, 'warning')).toHaveLength(2)
    expect(filterIssues(issues, 'all')).toHaveLength(5)
    expect(firstError(issues)?.code).toBe('E0')
    expect(firstError(filterIssues(issues, 'warning'))).toBeUndefined()
  })
  it('a parse error is one blocking issue with its line', () => {
    const p = parseIssue('bad', 3)
    expect(p).toMatchObject({ severity: 'error', line: 3, target: { kind: 'graph' } })
    expect(p.column).toBeUndefined()
  })
  it('a parse issue carries the column only when given one', () => {
    expect(parseIssue('bad', 3, 9)).toMatchObject({ line: 3, column: 9 })
  })
})

describe('badges match the diagnostics of the last result exactly', () => {
  it('counts per node, edge and port', () => {
    const diags = [
      d('error', 'E', { node_id: 'a' }),
      d('warning', 'W', { node_id: 'a', path: 'nodes.a.outputs.out' }),
      d('warning', 'W', { node_id: 'a', path: 'nodes.a.outputs.out' }),
      d('error', 'E', { edge_index: 0, node_id: 'b' }),
      d('error', 'E', { path: 'id' }),
    ]
    const b = badgesOf(diags, def)
    expect(b.nodes).toEqual({ a: { errors: 1, warnings: 2 }, b: { errors: 1, warnings: 0 } })
    expect(b.edges).toEqual({ 'a.outputs.out -> b.inputs.in': { errors: 1, warnings: 0 } })
    expect(b.ports).toEqual({ 'a.outputs.out': { errors: 0, warnings: 2 } })
  })
  it('no diagnostics, no badges; a dangling node gets none', () => {
    expect(badgesOf([], def)).toEqual({ nodes: {}, edges: {}, ports: {} })
    expect(badgesOf([d('error', 'E', { node_id: 'gone' })], def).nodes).toEqual({})
  })
})
