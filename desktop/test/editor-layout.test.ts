import { describe, expect, it } from 'vitest'
import { autoLayout, diagnosticCounts, freeSpot, parseRef, placements, readLayout, removalImpact, suggestId } from '../src/editor/layout'
import type { Definition } from '../src/editor/types'

const def = (metadata?: Record<string, unknown>): Definition => ({
  id: 'g',
  metadata,
  nodes: {
    a: { type: 'shell', outputs: { out: { type: 'string' } } },
    b: { type: 'shell', inputs: { in: { type: 'string' } } },
  },
  edges: [{ from: 'a.outputs.out', to: 'b.inputs.in' }],
  outputs: { done: { from: 'b.outputs.x' } },
})

describe('readLayout (ADR-0022, tolerant)', () => {
  it('reads integer positions of known nodes', () => {
    expect(readLayout(def({ layout: { nodes: { a: { x: 10, y: -20 } } } }))).toEqual({ a: { x: 10, y: -20 } })
  })
  it('ignores malformed entries without failing', () => {
    const l = { layout: { nodes: { a: { x: 1.5, y: 2 }, b: 'x', ghost: { x: 1, y: 1 }, c: null } } }
    expect(readLayout(def(l))).toEqual({})
    expect(readLayout(def({ layout: 'nope' }))).toEqual({})
    expect(readLayout(def({ layout: { nodes: { a: { x: 200000, y: 0 } } } }))).toEqual({})
    expect(readLayout(def())).toEqual({})
  })
})

describe('placements', () => {
  it('uses stored positions and falls back to the deterministic layout', () => {
    const d = def({ layout: { nodes: { a: { x: 5, y: 6 } } } })
    const p = placements(d)
    expect(p.a).toEqual({ x: 5, y: 6 })
    expect(p.b).toEqual(autoLayout(d).b)
    expect(autoLayout(d)).toEqual(autoLayout(d))
  })
  it('puts a consumer to the right of its provider', () => {
    const p = autoLayout(def())
    expect(p.b!.x).toBeGreaterThan(p.a!.x)
  })
})

describe('helpers', () => {
  it('parses references', () => {
    expect(parseRef('a.outputs.out')).toEqual({ node: 'a', side: 'outputs', port: 'out' })
    expect(parseRef('inputs.x')).toBeNull()
  })
  it('suggests the lowest free id', () => {
    expect(suggestId('shell', [])).toBe('shell_1')
    expect(suggestId('shell', ['shell_1', 'shell_3', 'agent_1'])).toBe('shell_2')
  })
  it('counts what a deletion removes', () => {
    expect(removalImpact(def(), 'b')).toEqual({ edges: 1, bindings: 0, outputs: 1 })
    expect(removalImpact(def(), 'a')).toEqual({ edges: 1, bindings: 0, outputs: 0 })
  })
  it('counts diagnostics per node', () => {
    const c = diagnosticCounts([
      { severity: 'error', code: 'E', message: '', location: { node_id: 'a' } },
      { severity: 'warning', code: 'W', message: '', location: { node_id: 'a' } },
      { severity: 'error', code: 'E', message: '' },
    ] as never)
    expect(c).toEqual({ a: { errors: 1, warnings: 1 } })
  })
  it('steps past nodes already on the spot', () => {
    const first = freeSpot({ x: 500, y: 300 }, [])
    expect(freeSpot({ x: 500, y: 300 }, [first])).toEqual({ x: first.x + 32, y: first.y + 32 })
  })
})
