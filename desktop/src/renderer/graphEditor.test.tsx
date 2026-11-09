import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { GraphEditor, nextNodeID, nodeReferenceCount } from './graphEditor'
import type { GraphDocumentController, GraphDocumentState } from './graphDocument'

const state: GraphDocumentState = {
  document: { document_id: 'doc', project_id: 'project', relative_path: 'graphs/fix.yaml',
    text: 'schema_version: 0\nid: fix-issue\n', revision: 3, disk_digest: 'digest', validity: 'valid' },
  draftText: 'schema_version: 0\nid: fix-issue\n', pendingUpdates: 0,
  inspection: { revision: 3, validity: 'valid', diagnostics: { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false },
    graph: { schema_version: 0, id: 'fix-issue', units: [], inputs: [],
      nodes: [{ id: 'plan', type: 'agent', inputs: [], outputs: [{ name: 'summary', type: 'string', required: true }], needs_configuration: false }],
      edges: [], graph_input_bindings: [], outputs: [] },
    layout: { version: 1, source: 'fallback', nodes: { plan: { x: 10, y: 20 } } } },
  lastValid: null, preview: null, dirty: false, staleCanvas: false, canVisualEdit: true,
  canStart: true, canUndo: false, canRedo: false, externalConflict: false, reformatPending: false, error: null
}

function fakeController(snapshot: GraphDocumentState): GraphDocumentController {
  return { getSnapshot: () => snapshot, subscribe: () => () => {} } as unknown as GraphDocumentController
}

describe('graph editor surface', () => {
  it('offers all three modes and reflects the saved valid graph without claiming unsupported actions', () => {
    const html = renderToStaticMarkup(<GraphEditor controller={fakeController(state)} onClose={() => {}} />)
    expect(html).toContain('Visual')
    expect(html).toContain('YAML')
    expect(html).toContain('Split')
    expect(html).toContain('Graph canvas')
    expect(html).toContain('schema_version: 0')
    expect(html).toContain('Run graph')
    expect(html).toContain('disabled=""')
    expect(html).toContain('Human')
  })

  it('keeps an invalid draft visible and marks the visual projection stale', () => {
    const invalid: GraphDocumentState = { ...state, draftText: 'nodes: [', pendingUpdates: 0,
      document: { ...state.document, text: 'nodes: [', revision: 4, validity: 'invalid' },
      inspection: { revision: 4, validity: 'invalid', diagnostics: { diagnostics: [], diagnostics_total: 1, diagnostics_omitted: 0, has_errors: true } },
      lastValid: state.inspection, dirty: true, staleCanvas: true, canVisualEdit: false, canStart: false }
    const html = renderToStaticMarkup(<GraphEditor controller={fakeController(invalid)} onClose={() => {}} />)
    expect(html).toContain('nodes: [')
    expect(html).toContain('Stale canvas')
    expect(html).toContain('Unsaved')
  })

  it('counts references before deleting and proposes deterministic node IDs', () => {
    const graph = state.inspection!.graph!
    expect(nextNodeID(graph, 'shell')).toBe('shell_1')
    expect(nextNodeID({ ...graph, nodes: [...graph.nodes, { id: 'shell_1', type: 'shell', inputs: [], outputs: [], needs_configuration: true }] }, 'shell')).toBe('shell_2')
    expect(nodeReferenceCount({ ...graph, edges: [{ from: 'plan.outputs.summary', to: 'other.inputs.task' }],
      outputs: [{ name: 'summary', from: 'plan.outputs.summary' }] }, 'plan')).toBe(2)
  })
})
