import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { GraphCanvas, clampCanvasZoom, graphEdgeEndpoints, movedCanvasPosition } from './graphCanvas'
import type { GraphInspection } from './graphDocument'

const inspection: GraphInspection = {
  revision: 3, validity: 'valid',
  diagnostics: { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false },
  graph: {
    schema_version: 0, id: 'fix-issue', units: ['api'], inputs: [],
    nodes: [
      { id: 'plan', type: 'agent', inputs: [], outputs: [{ name: 'summary', type: 'string', required: true }], needs_configuration: false },
      { id: 'verify', type: 'gate', inputs: [{ name: 'task', type: 'string', required: true }], outputs: [{ name: 'verdict', type: 'boolean', required: true }], needs_configuration: true }
    ],
    edges: [{ from: 'plan.outputs.summary', to: 'verify.inputs.task' }], graph_input_bindings: [], outputs: []
  },
  layout: { version: 1, source: 'fallback', nodes: { plan: { x: 10, y: 20 }, verify: { x: 270, y: 20 } } }
}

describe('graph canvas', () => {
  it('shows actual typed ports and dependency paths as inert text', () => {
    const html = renderToStaticMarkup(<GraphCanvas inspection={inspection} selectedID="plan" disabled={false}
      onSelect={() => {}} onDelete={() => {}} onMove={() => {}} onResetLayout={() => {}} />)
    expect(html).toContain('plan.outputs.summary')
    expect(html).toContain('verify.inputs.task')
    expect(html).toContain('summary · string')
    expect(html).toContain('task · string')
    expect(html).toContain('Needs configuration')
    expect(html).toContain('aria-label="Graph canvas"')
    expect(html).toContain('aria-selected="true"')
  })

  it('renders an invalid revision only as a stale, non-editable canvas', () => {
    const html = renderToStaticMarkup(<GraphCanvas inspection={{ ...inspection, validity: 'invalid' }} selectedID={null} disabled
      stale onSelect={() => {}} onDelete={() => {}} onMove={() => {}} onResetLayout={() => {}} />)
    expect(html).toContain('Stale canvas')
    expect(html).toContain('aria-disabled="true"')
    expect(html).not.toContain('Reset layout</button>')
  })

  it('maps only declared node-to-node edges and bounds viewport operations', () => {
    expect(graphEdgeEndpoints(inspection.graph!, inspection.layout!)).toEqual([{
      from: 'plan.outputs.summary', to: 'verify.inputs.task', x1: 190, y1: 64, x2: 270, y2: 64
    }])
    expect(clampCanvasZoom(0.1)).toBe(0.5)
    expect(clampCanvasZoom(10)).toBe(2)
    expect(movedCanvasPosition({ x: 99995, y: -99995 }, 100, -100, 1)).toEqual({ x: 100000, y: -100000 })
  })
})
