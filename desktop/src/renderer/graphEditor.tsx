import { useEffect, useState, useSyncExternalStore } from 'react'
import type { GraphDocumentController, GraphEditOperation, GraphProjection } from './graphDocument'
import { GraphCanvas } from './graphCanvas'

type Mode = 'visual' | 'yaml' | 'split'

export function nextNodeID(graph: GraphProjection, type: 'shell' | 'agent' | 'gate'): string {
  const existing = new Set(graph.nodes.map((node) => node.id))
  for (let number = 1; number <= 10000; number++) {
    const candidate = `${type}_${number}`
    if (!existing.has(candidate)) return candidate
  }
  throw new Error('Graph has no available node identifier')
}

export function nodeReferenceCount(graph: GraphProjection, nodeID: string): number {
  const prefix = `${nodeID}.`
  return graph.edges.filter((edge) => edge.from.startsWith(prefix) || edge.to.startsWith(prefix)).length +
    graph.graph_input_bindings.filter((binding) => binding.to.startsWith(prefix)).length +
    graph.outputs.filter((output) => output.from.startsWith(prefix)).length
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Graph action unavailable'
}

export function GraphEditor({ controller, onClose, serviceReady = true }: { controller: GraphDocumentController; onClose: () => void; serviceReady?: boolean }): React.JSX.Element {
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot)
  const [mode, setMode] = useState<Mode>('split')
  const [selectedID, setSelectedID] = useState<string | null>(() => state.inspection?.graph?.nodes[0]?.id ?? null)
  const [busy, setBusy] = useState(false)
  const [localError, setLocalError] = useState<string | null>(null)
  const [deletePending, setDeletePending] = useState<string | null>(null)
  const [closePending, setClosePending] = useState(false)
  const [reloadPending, setReloadPending] = useState(false)
  const [replaceLayoutPending, setReplaceLayoutPending] = useState<Extract<GraphEditOperation, { kind: 'set_layout' }> | null>(null)
  const [resetUnknownPending, setResetUnknownPending] = useState(false)
  const [resetRemaining, setResetRemaining] = useState<string[]>([])

  const canvasInspection = state.inspection?.graph ? state.inspection : state.lastValid
  const graph = canvasInspection?.graph
  const selected = graph?.nodes.find((node) => node.id === selectedID) ?? null
  useEffect(() => {
    if (selectedID && graph?.nodes.some((node) => node.id === selectedID)) return
    setSelectedID(graph?.nodes[0]?.id ?? null)
  }, [graph, selectedID])

  async function action(work: () => Promise<void>): Promise<void> {
    if (busy) return
    setBusy(true)
    setLocalError(null)
    try { await work() }
    catch (cause) { setResetRemaining([]); setLocalError(errorText(cause)) }
    finally { setBusy(false) }
  }

  async function preview(operation: GraphEditOperation): Promise<void> {
    const next = await controller.previewVisual(operation)
    if (!next.reformat_required) await controller.acceptPreview(false)
  }

  function addNode(type: 'shell' | 'agent' | 'gate'): void {
    if (!graph || !state.canVisualEdit) return
    const id = nextNodeID(graph, type)
    void action(async () => { await preview({ kind: 'add_node', node_id: id, node_type: type }); setSelectedID(id) })
  }

  function deleteNode(id: string): void {
    if (!graph || !state.canVisualEdit) return
    if (nodeReferenceCount(graph, id) > 0) { setDeletePending(id); return }
    void action(() => preview({ kind: 'delete_node', node_id: id, remove_references: false }))
  }

  function moveNode(id: string, x: number, y: number): void {
    if (!state.canVisualEdit) return
    const operation: Extract<GraphEditOperation, { kind: 'set_layout' }> = { kind: 'set_layout', node_id: id, x, y }
    if (canvasInspection?.layout?.source === 'unsupported_version') {
      setReplaceLayoutPending(operation)
      return
    }
    void action(() => preview(operation))
  }

  async function resetLayout(replaceUnknown: boolean): Promise<void> {
    if (!graph || !state.canVisualEdit) return
    const ids = resetRemaining.length ? resetRemaining : graph.nodes.map((node) => node.id)
    setResetRemaining([])
    for (let index = 0; index < ids.length; index++) {
      const next = await controller.previewVisual({ kind: 'remove_layout', node_id: ids[index],
        ...(replaceUnknown && index === 0 ? { replace_unknown_layout: true } : {}) })
      if (next.reformat_required) { setResetRemaining(ids.slice(index + 1)); return }
      await controller.acceptPreview(false)
    }
  }

  function requestResetLayout(): void {
    if (!state.canVisualEdit || !graph) return
    if (canvasInspection?.layout?.source === 'unsupported_version') { setResetUnknownPending(true); return }
    void action(() => resetLayout(false))
  }

  function reloadDisk(): void {
    if (state.dirty || state.pendingUpdates > 0) { setReloadPending(true); return }
    void action(() => controller.reloadDisk(false))
  }

  const status = state.externalConflict ? 'External edit conflict' : state.pendingUpdates > 0 ? 'Updating draft' : state.dirty ? 'Unsaved' : 'Saved'
  const visualBlocked = !state.canVisualEdit || busy || state.reformatPending || !serviceReady
  const visibleDiagnostics = state.inspection?.diagnostics.diagnostics ?? []
  return <section className="graph-editor" aria-label="Graph editor">
    <header className="graph-editor-toolbar">
      <button type="button" className="small-button" onClick={() => {
        if (state.dirty || state.pendingUpdates > 0) setClosePending(true)
        else onClose()
      }}>Back to project</button>
      <div className="graph-editor-title"><strong>{graph?.id ?? state.document.relative_path}</strong><span>{status} · schema 0</span></div>
      <button type="button" className="small-button" disabled={!serviceReady || busy || state.pendingUpdates > 0 || !state.canUndo} onClick={() => void action(() => controller.undo())}>Undo</button>
      <button type="button" className="small-button" disabled={!serviceReady || busy || state.pendingUpdates > 0 || !state.canRedo} onClick={() => void action(() => controller.redo())}>Redo</button>
      <button type="button" className="small-button" disabled={!serviceReady || busy || state.pendingUpdates > 0} onClick={reloadDisk}>Reload disk</button>
      <button type="button" className="small-button" disabled={!serviceReady || busy || state.pendingUpdates > 0 || state.reformatPending || !state.dirty} onClick={() => void action(() => controller.save())}>Save</button>
      <button type="button" className="small-button" disabled title="Validation details arrive with F2.4">Validate</button>
      <button type="button" className="small-button" disabled title="Run setup arrives with F2.4">Run graph</button>
    </header>
    {(state.error || localError) && <div className="graph-editor-error" role="alert">{localError ?? state.error}</div>}
    {!serviceReady && <div className="graph-editor-error" role="status">Local service unavailable. This graph remains visible but editing is paused.</div>}
    {closePending && <div className="graph-editor-choice" role="dialog" aria-label="Close graph confirmation">Close this graph and discard its unsaved draft?
      <button type="button" onClick={() => setClosePending(false)}>Keep editing</button>
      <button type="button" disabled={!serviceReady || busy} onClick={() => void action(async () => { await controller.reloadDisk(true); onClose() })}>Discard draft and close</button>
    </div>}
    {state.externalConflict && <div className="graph-editor-choice" role="alert">The saved file changed outside this editor. Keep the draft or reload the disk version.
      <button type="button" onClick={() => { controller.keepDraft(); setLocalError(null) }}>Keep draft</button>
      <button type="button" onClick={() => setReloadPending(true)}>Reload disk</button>
    </div>}
    {reloadPending && <div className="graph-editor-choice" role="dialog" aria-label="Reload disk confirmation">Reload the disk version and discard this draft?
      <button type="button" onClick={() => setReloadPending(false)}>Keep draft</button>
      <button type="button" disabled={!serviceReady} onClick={() => { setReloadPending(false); void action(() => controller.reloadDisk(true)) }}>Discard draft and reload</button>
    </div>}
    {deletePending && <div className="graph-editor-choice" role="dialog" aria-label="Delete node confirmation">Delete {deletePending} and {graph ? nodeReferenceCount(graph, deletePending) : 0} declared references?
      <button type="button" onClick={() => setDeletePending(null)}>Keep node</button>
      <button type="button" onClick={() => { const id = deletePending; setDeletePending(null); void action(() => preview({ kind: 'delete_node', node_id: id, remove_references: true })) }}>Delete node and references</button>
    </div>}
    {replaceLayoutPending && <div className="graph-editor-choice" role="dialog" aria-label="Replace unknown layout confirmation">This graph has a newer layout version. Review replacement before changing its position.
      <button type="button" onClick={() => setReplaceLayoutPending(null)}>Keep layout</button>
      <button type="button" onClick={() => { const operation = replaceLayoutPending; setReplaceLayoutPending(null); void action(() => preview({ ...operation, replace_unknown_layout: true })) }}>Preview replacement</button>
    </div>}
    {resetUnknownPending && <div className="graph-editor-choice" role="dialog" aria-label="Reset unknown layout confirmation">Reset layout will replace metadata from a newer editor version.
      <button type="button" onClick={() => setResetUnknownPending(false)}>Keep layout</button>
      <button type="button" onClick={() => { setResetUnknownPending(false); void action(() => resetLayout(true)) }}>Preview reset</button>
    </div>}
    {state.preview?.reformat_required && <div className="graph-editor-choice graph-editor-reformat" role="dialog" aria-label="YAML reformat preview">
      <strong>Review the full YAML change</strong><p>Changed paths: {state.preview.changed_paths.join(', ') || 'formatting'}</p>
      <pre>{state.preview.text}</pre>
      <button type="button" onClick={() => { controller.discardPreview(); setResetRemaining([]) }}>Keep current YAML</button>
      <button type="button" disabled={!serviceReady} onClick={() => void action(async () => { await controller.acceptPreview(true); if (resetRemaining.length) await resetLayout(false) })}>Apply reviewed change</button>
    </div>}
    <div className="graph-editor-tabs" role="group" aria-label="Editor mode">
      {(['visual', 'yaml', 'split'] as const).map((option) => <button key={option} type="button" aria-pressed={mode === option}
        onClick={() => setMode(option)}>{option === 'yaml' ? 'YAML' : option === 'visual' ? 'Visual' : 'Split'}</button>)}
    </div>
    <div className={`graph-editor-body mode-${mode}`}>
      {(mode === 'yaml' || mode === 'split') && <div className="graph-yaml-pane"><div className="graph-pane-title">YAML · {state.document.relative_path}</div>
        <textarea aria-label="Graph YAML" spellCheck={false} value={state.draftText} readOnly={!serviceReady}
          onChange={(event) => { setLocalError(null); void controller.updateText(event.target.value).catch((cause) => setLocalError(errorText(cause))) }} /></div>}
      {(mode === 'visual' || mode === 'split') && <div className="graph-visual-pane"><div className="graph-add-actions">
        <button type="button" disabled={visualBlocked} onClick={() => addNode('agent')}>+ Agent</button>
        <button type="button" disabled={visualBlocked} onClick={() => addNode('shell')}>+ Shell</button>
        <button type="button" disabled={visualBlocked} onClick={() => addNode('gate')}>+ Gate</button>
        <button type="button" disabled title="Human nodes arrive with F2.10">+ Human</button>
      </div>{canvasInspection && <GraphCanvas inspection={canvasInspection} selectedID={selectedID}
        disabled={visualBlocked} stale={state.staleCanvas || state.pendingUpdates > 0}
        onSelect={setSelectedID} onDelete={deleteNode} onMove={moveNode} onResetLayout={requestResetLayout} />}</div>}
      <aside className="graph-node-inspector" aria-label="Node summary"><div className="graph-pane-title">NODE · {selected?.type.toUpperCase() ?? 'NONE'}</div>
        {selected ? <><h2>{selected.id}</h2><h3>Ports</h3>
          {selected.inputs.map((port) => <div className="graph-inspector-port" key={`in:${port.name}`}><span>{port.name}</span><span>input · {port.type}</span></div>)}
          {selected.outputs.map((port) => <div className="graph-inspector-port" key={`out:${port.name}`}><span>{port.name}</span><span>output · {port.type}</span></div>)}
          <h3>Execution</h3><div>Type {selected.type}</div>{selected.loop_max_attempts && <div>Loop · {selected.loop_max_attempts} attempts max</div>}
          {selected.needs_configuration && <p>Needs configuration in YAML before this graph can run.</p>}
          <button type="button" className="small-button" disabled={visualBlocked} onClick={() => deleteNode(selected.id)}>Delete node</button>
          <p>Detailed inspector controls arrive with F2.2.</p></> : <p>Select a node to inspect its ports.</p>}
      </aside>
    </div>
    <footer className="graph-editor-issues"><strong>Issues · {state.inspection?.diagnostics.diagnostics_total ?? 0}</strong>
      {visibleDiagnostics.length ? visibleDiagnostics.map((diagnostic, index) => <span key={`${diagnostic.code}-${index}`}>{diagnostic.severity} {diagnostic.code}: {diagnostic.message}</span>)
        : <span>{state.inspection?.validity === 'invalid' ? 'The current YAML is invalid.' : 'Validate this saved graph to inspect issues.'}</span>}
    </footer>
  </section>
}
