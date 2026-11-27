import { Background, BackgroundVariant, MarkerType, MiniMap, ReactFlow, ReactFlowProvider, applyNodeChanges, useNodesInitialized, useReactFlow, type Connection, type Edge, type Node, type NodeChange, type OnConnectStart } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import { sourceType } from './bindings'
import { Dock, ViewControls, type Tool } from './CanvasChrome'
import { listConnections, verdictsOf } from './connections'
import type { DocState } from './doc'
import { EmptyGraph } from './EmptyGraph'
import { usePresence } from './Menu'
import { badgesOf, type FocusReq } from './issues'
import { NODE_W, clearSpot, makeRef, parseRef, placements, removalImpact, suggestId, type Point } from './layout'
import type { SelectionOwner } from './selection'
import { CanvasCtx, StepCard, type CanvasState, type StepData } from './StepCard'
import { edgeTypes, type StepEdgeData } from './StepEdge'
import { StepMenu, type StepMenuAnchor } from './StepMenu'
import type { Definition, Op, QueryResult } from './types'
import type { GraphDocument } from './useGraphDocument'
import { useResolvedTheme } from '../theme'

// The canvas (graph editor v2) draws the definition the engine returned and turns
// gestures into ADR-0020 operations. Whether a connection is allowed is the
// engine's answer (can_connect); nothing here decides it. Node objects are static:
// selection, diagnostics and connect hints reach the cards through context, because
// replacing node objects makes React Flow drop its handle measurements and draw no edges.

const portNames = (ports: Record<string, unknown> | undefined) => Object.keys(ports ?? {})

function desiredNodes(def: Definition, pos: Record<string, Point>): Node<StepData>[] {
  return Object.keys(def.nodes)
    .sort()
    .map((id) => {
      const n = def.nodes[id]!
      return { id, type: 'step', position: pos[id] ?? { x: 0, y: 0 }, data: { id, kind: n.type, sig: JSON.stringify([n.type, portNames(n.inputs), portNames(n.outputs)]) }, connectable: true }
    })
}

/** Keeps node objects that did not change, so React Flow keeps their measurements. */
function mergeNodes(prev: Node<StepData>[], next: Node<StepData>[]): Node<StepData>[] {
  const old = new Map(prev.map((n) => [n.id, n]))
  return next.map((n) => {
    const p = old.get(n.id)
    if (!p || p.data.sig !== n.data.sig) return n
    if (p.position.x === n.position.x && p.position.y === n.position.y) return p
    return { ...p, position: n.position }
  })
}

type Mode = null | { kind: 'rename' } | { kind: 'delete' } | { kind: 'connect'; from?: string; checks?: Record<string, QueryResult> }

export type CanvasBlock = 'parse' | 'anchors' | null

const nodeTypes = { step: StepCard }

export interface CanvasProps {
  g: GraphDocument
  doc: DocState
  focusReq: FocusReq | null
  selection: SelectionOwner
  /** "Or write it in the YAML view" from the empty graph. */
  onYaml: () => void
}

export function GraphCanvas(props: CanvasProps) {
  return (
    <ReactFlowProvider>
      <Inner {...props} />
    </ReactFlowProvider>
  )
}

function Inner({ g, doc, focusReq, selection, onYaml }: CanvasProps) {
  const { t } = useI18n()
  const theme = useResolvedTheme()
  const def = doc.analysis.definition
  // A text that does not parse is also reported read-only by the engine; the parse error is the useful reason.
  const block: CanvasBlock = doc.analysis.parseError ? 'parse' : doc.analysis.readonlyReasons?.length ? 'anchors' : null
  const readOnly = block !== null || !def
  const flow = useReactFlow()
  const sel = selection.selection
  const setSel = selection.select
  const [mode, setMode] = useState<Mode>(null)
  const [renameTo, setRenameTo] = useState('')
  const [notice, setNotice] = useState<string | null>(null)
  const [reasons, setReasons] = useState<Record<string, QueryResult>>({})
  const reasonsRef = useRef(reasons)
  reasonsRef.current = reasons
  const [nodes, setNodes] = useState<Node<StepData>[]>([])
  const [resetKey, setResetKey] = useState(0)
  const dragging = useRef(false)
  const [tool, setTool] = useState<Tool>('select')
  const [minimap, setMinimap] = useState(false)
  const [menu, setMenu] = useState<StepMenuAnchor | null>(null)
  const wrap = useRef<HTMLDivElement>(null)

  const desired = useMemo(() => (def ? desiredNodes(def, placements(def)) : []), [def])
  useEffect(() => {
    if (dragging.current) return
    setNodes((prev) => mergeNodes(prev, desired))
  }, [desired, resetKey])

  const badges = useMemo(() => badgesOf(doc.analysis.diagnostics, def), [doc.analysis.diagnostics, def])
  const [focusPort, setFocusPort] = useState<string | null>(null)
  // A graph input binding has no edge to draw: its target node is the one outlined.
  const outlined = sel?.kind === 'node' ? sel.id : sel?.kind === 'binding' ? (parseRef(sel.to)?.node ?? null) : null
  const openMenu = useCallback((id: string, el: HTMLElement) => {
    setSel({ kind: 'node', id })
    setMenu((m) => (m?.id === id && m.anchor.kind === 'element' ? null : { id, anchor: { kind: 'element', el, align: 'start' } }))
  }, [setSel])
  const ctx = useMemo<CanvasState>(
    () => ({ def, nodeTypes: doc.analysis.nodeTypes, selectedNode: outlined, badges, focusPort, readOnly, reasons, menuFor: menu?.id ?? null, openMenu }),
    [def, doc.analysis.nodeTypes, outlined, badges, focusPort, readOnly, reasons, menu?.id, openMenu],
  )

  // An issue was chosen (the editor already selected its element): outline its port.
  useEffect(() => {
    if (!focusReq || !def) return
    const tg = focusReq.target
    setFocusPort(tg.kind === 'port' ? makeRef(tg.node, tg.side, tg.port) : tg.kind === 'binding' ? tg.to : null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusReq?.nonce])

  const edges = useMemo<Edge<StepEdgeData>[]>(
    () =>
      (def?.edges ?? []).flatMap((e) => {
        const a = parseRef(e.from)
        const b = parseRef(e.to)
        if (!a || !b) return []
        const selected = sel?.kind === 'edge' && sel.from === e.from && sel.to === e.to
        const badge = badges.edges[`${e.from} -> ${e.to}`]
        const issue = badge ? (badge.errors > 0 ? 'error' : 'warning') : undefined
        const issueText = badge ? (badge.errors > 0 ? t('editor.nodeErrors', { count: badge.errors }) : t('editor.nodeWarnings', { count: badge.warnings })) : undefined
        return [
          {
            id: `${e.from}->${e.to}`,
            type: 'step',
            source: a.node,
            target: b.node,
            sourceHandle: `out:${a.port}`,
            targetHandle: `in:${b.port}`,
            selected,
            ariaLabel: `${e.from} → ${e.to}`,
            className: selected ? 'ed-edge-sel' : '',
            markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
            // A selected edge says what it carries (F2.3 requirement 8).
            data: { port: a.port, type: def ? sourceType(def, e.from) : undefined, issue, issueText },
          },
        ]
      }),
    [def, sel, badges, t],
  )

  const run = useCallback(
    async (ops: Op[]) => {
      setNotice(null)
      const r = await g.applyOps(ops)
      if (!r.ok) setResetKey((k) => k + 1) // put dragged nodes back where the file says they are
      return r
    },
    [g],
  )

  // ---- operations -------------------------------------------------------------
  const addNode = (type: string) => {
    if (!def) return
    const box = wrap.current?.getBoundingClientRect()
    const centre = box ? flow.screenToFlowPosition({ x: box.left + box.width / 2, y: box.top + box.height / 2 }) : { x: 0, y: 0 }
    // The new card never covers another one (cards are measured; an unmeasured one counts at its estimated size).
    const rects = flow.getNodes().map((n) => ({ x: n.position.x, y: n.position.y, w: n.measured?.width ?? NODE_W, h: n.measured?.height ?? 160 }))
    const spot = clearSpot(centre, rects, { w: NODE_W, h: 160 })
    const id = suggestId(type, Object.keys(def.nodes))
    void run([{ op: 'add_node', id, type }, { op: 'set_layout', nodes: { [id]: spot } }]).then((r) => r.ok && setSel({ kind: 'node', id }))
  }

  const arrange = () => {
    if (!def) return
    void run([{ op: 'set_layout', nodes: placementsAuto(def) }]).then((r) => r.ok && requestAnimationFrame(() => void flow.fitView({ padding: 0.2, duration: 300 })))
  }

  const move = (id: string, p: Point) => void run([{ op: 'set_layout', nodes: { [id]: { x: Math.round(p.x), y: Math.round(p.y) } } }])

  const connect = (from: string, to: string) => run([{ op: 'add_edge', from, to }])

  const queryTargets = useCallback(
    async (from: string): Promise<Record<string, QueryResult>> => {
      if (!def) return {}
      const a = await listConnections((ops) => g.applyOps(ops, { quiet: true }), { from })
      if (a.ok) return verdictsOf(a.options)
      setNotice(t('editor.connectTargetsFailed', { message: a.message }))
      return {}
    },
    [def, g, t],
  )

  // The engine's verdicts for the output being dragged; a drop can arrive before they do.
  const pending = useRef<{ from: string; verdicts: Promise<Record<string, QueryResult>> } | null>(null)
  const onConnectStart: OnConnectStart = (_, p) => {
    if (p.handleType !== 'source' || !p.nodeId || !p.handleId?.startsWith('out:')) return
    const from = makeRef(p.nodeId, 'outputs', p.handleId.slice(4))
    setReasons({})
    const verdicts = queryTargets(from)
    pending.current = { from, verdicts }
    void verdicts.then((v) => pending.current?.from === from && setReasons(v))
  }

  const refsOf = (c: Connection): { from: string; to: string } | null =>
    c.sourceHandle?.startsWith('out:') && c.targetHandle?.startsWith('in:') ? { from: makeRef(c.source, 'outputs', c.sourceHandle.slice(4)), to: makeRef(c.target, 'inputs', c.targetHandle.slice(3)) } : null

  const isValidConnection = (c: Connection | Edge) => {
    const r = refsOf(c as Connection)
    return r !== null && reasonsRef.current[r.to]?.ok === true
  }

  const onConnect = (c: Connection) => {
    const r = refsOf(c)
    if (r) void connect(r.from, r.to)
  }

  const onConnectEnd = (_: unknown, state: { isValid: boolean | null; toHandle: { nodeId: string; id?: string | null } | null }) => {
    const drag = pending.current
    pending.current = null
    setReasons({})
    if (state.isValid !== false || !drag || !state.toHandle?.id?.startsWith('in:')) return
    const to = makeRef(state.toHandle.nodeId, 'inputs', state.toHandle.id.slice(3))
    const known = reasonsRef.current[to]
    // Dropped before the engine answered: wait for its verdict, then connect or say why not.
    void (known ? Promise.resolve(known) : drag.verdicts.then((v) => v[to])).then((verdict) => {
      if (verdict?.ok && !known) void connect(drag.from, to)
      else if (verdict && !verdict.ok && verdict.message) setNotice(t('editor.cannotConnect', { message: verdict.message }))
    })
  }

  const onNodesChange = (changes: NodeChange<Node<StepData>>[]) => setNodes((n) => applyNodeChanges(changes, n))

  const selectedId = sel?.kind === 'node' ? sel.id : null
  const selectedNode = selectedId && def ? def.nodes[selectedId] : undefined
  const impact = def && selectedId ? removalImpact(def, selectedId) : null

  const startRename = () => {
    setRenameTo(selectedId ?? '')
    setMode({ kind: 'rename' })
  }
  const startDelete = () => {
    if (!selectedId) return
    if (impact && impact.edges + impact.bindings + impact.outputs > 0) setMode({ kind: 'delete' })
    else void run([{ op: 'remove_node', id: selectedId }]).then((r) => r.ok && setSel(null))
  }
  const pickOutput = async (port: string) => {
    if (!selectedId) return
    const from = makeRef(selectedId, 'outputs', port)
    setMode({ kind: 'connect', from })
    const checks = await queryTargets(from)
    setMode((m) => (m && m.kind === 'connect' && m.from === from ? { kind: 'connect', from, checks } : m))
  }

  // F2 renames and Delete removes the selected step, from anywhere on the canvas but a text field.
  // A native listener: the canvas is a container, not a control.
  const canvasRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = canvasRef.current
    if (!el) return
    el.addEventListener('keydown', onKeyDown)
    return () => el.removeEventListener('keydown', onKeyDown)
  })
  const onKeyDown = (e: KeyboardEvent) => {
    if (readOnly || !selectedId || (e.target as HTMLElement).closest('input,textarea,select,[role="menu"]')) return
    if (e.key === 'F2') {
      e.preventDefault()
      startRename()
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault()
      startDelete()
    }
  }

  const empty = !!def && Object.keys(def.nodes).length === 0
  const floatOpen = (mode?.kind === 'rename' || mode?.kind === 'delete' || mode?.kind === 'connect') && !!selectedId
  const float = usePresence(floatOpen)
  const banner = notice ?? (block ? (block === 'parse' ? t('editor.readonly.parse', { message: doc.analysis.parseError ?? '' }) : t('editor.readonly.anchors')) : null)

  return (
    <div ref={canvasRef} className={`er-cv tool-${tool}`} data-testid="canvas-card">
      <div className="ed-flow" ref={wrap} data-testid="editor-canvas" role="region" aria-label={t('editor.canvasLabel')}>
        <CanvasCtx.Provider value={ctx}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            onNodesChange={onNodesChange}
            fitView
            fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
            minZoom={0.25}
            maxZoom={2}
            nodesDraggable={!readOnly && tool === 'select'}
            nodesConnectable={!readOnly}
            elementsSelectable={tool === 'select'}
            panOnDrag={tool === 'pan' ? true : [1, 2]}
            panActivationKeyCode="Space"
            selectionOnDrag={false}
            edgesFocusable
            deleteKeyCode={null}
            proOptions={{ hideAttribution: true }}
            onNodeClick={(_, n) => setSel({ kind: 'node', id: n.id })}
            onNodeContextMenu={(e, n) => {
              e.preventDefault()
              setSel({ kind: 'node', id: n.id })
              setMenu({ id: n.id, anchor: { kind: 'point', x: e.clientX, y: e.clientY } })
            }}
            onEdgeClick={(_, e) => {
              const a = e.sourceHandle?.startsWith('out:') ? makeRef(e.source, 'outputs', e.sourceHandle.slice(4)) : ''
              const b = e.targetHandle?.startsWith('in:') ? makeRef(e.target, 'inputs', e.targetHandle.slice(3)) : ''
              setSel({ kind: 'edge', from: a, to: b })
            }}
            onPaneClick={() => setSel(null)}
            onMoveStart={() => setMenu(null)}
            onNodeDragStart={() => {
              dragging.current = true
              setMenu(null)
            }}
            onNodeDragStop={(_, n) => {
              dragging.current = false
              move(n.id, n.position)
            }}
            onConnectStart={onConnectStart}
            onConnect={onConnect}
            onConnectEnd={onConnectEnd}
            isValidConnection={isValidConnection}
            colorMode={theme}
          >
            <Background variant={BackgroundVariant.Dots} gap={20} size={1.2} />
            {minimap && <MiniMap className="v2-minimap" pannable zoomable ariaLabel={t('editor.v2.minimap')} nodeBorderRadius={6} />}
            <FitOnResize target={wrap} />
          </ReactFlow>
        </CanvasCtx.Provider>
      </div>

      {!def && <div className="ed-empty mu">{t('editor.canvas.noGraph')}</div>}
      {empty && <EmptyGraph onAdd={addNode} onYaml={onYaml} disabled={readOnly} />}

      {banner && (
        <div className={`v2-toast${block && !notice ? ' warn' : ''}`} role="status" data-testid={notice ? 'canvas-notice' : 'canvas-readonly'}>
          <span>{banner}</span>
          {notice && (
            <Button size="sm" className="ghost" onClick={() => setNotice(null)}>
              {t('editor.dismiss')}
            </Button>
          )}
        </div>
      )}

      {float.mounted && (
        <div className={`v2-float ${float.phase}`}>
          {mode?.kind === 'rename' && selectedId && (
            <form
              className="v2-float-row"
              data-testid="rename-form"
              onSubmit={(e) => {
                e.preventDefault()
                const next = renameTo.trim()
                if (!next || next === selectedId) return setMode(null)
                void run([{ op: 'rename_node', id: selectedId, new_id: next }]).then((r) => {
                  if (r.ok) {
                    setSel({ kind: 'node', id: next })
                    setMode(null)
                  }
                })
              }}
              onKeyDown={(e) => e.key === 'Escape' && setMode(null)}
            >
              <label htmlFor="rename-input">{t('editor.rename.label', { node: selectedId })}</label>
              <input id="rename-input" className="er-in mono" value={renameTo} onChange={(e) => setRenameTo(e.target.value)} autoFocus data-testid="rename-input" />
              <Button size="sm" variant="primary" type="submit" data-testid="rename-apply">
                {t('editor.rename.apply')}
              </Button>
              <Button size="sm" onClick={() => setMode(null)}>
                {t('editor.cancel')}
              </Button>
            </form>
          )}
          {mode?.kind === 'delete' && selectedId && impact && (
            <div className="v2-float-row" role="alertdialog" aria-label={t('editor.delete')} data-testid="delete-confirm">
              <span>{t('editor.delete.confirm', { node: selectedId, edges: impact.edges, bindings: impact.bindings, outputs: impact.outputs })}</span>
              <Button
                size="sm"
                variant="danger"
                data-testid="delete-apply"
                autoFocus
                onClick={() =>
                  void run([{ op: 'remove_node', id: selectedId }]).then((r) => {
                    if (r.ok) {
                      setSel(null)
                      setMode(null)
                    }
                  })
                }
              >
                {t('editor.delete')}
              </Button>
              <Button size="sm" onClick={() => setMode(null)}>
                {t('editor.cancel')}
              </Button>
            </div>
          )}
          {mode?.kind === 'connect' && selectedId && selectedNode && (
            <div className="v2-float-col" data-testid="connect-panel">
              {!mode.from ? (
                <div className="v2-float-row">
                  <span>{t('editor.connect.pickOutput', { node: selectedId })}</span>
                  {portNames(selectedNode.outputs).map((p) => (
                    <Button key={p} size="sm" onClick={() => void pickOutput(p)} data-testid={`connect-out-${p}`}>
                      <span className="mono">{p}</span> <span className="mu">{selectedNode.outputs?.[p]?.type}</span>
                    </Button>
                  ))}
                  <Button size="sm" className="ghost" onClick={() => setMode(null)}>
                    {t('editor.cancel')}
                  </Button>
                </div>
              ) : (
                <>
                  <div className="v2-float-row">
                    <span>{t('editor.connect.pickTarget', { from: mode.from })}</span>
                    <Button size="sm" className="ghost" onClick={() => setMode(null)}>
                      {t('editor.cancel')}
                    </Button>
                  </div>
                  {!mode.checks && <div className="mu">{t('editor.connect.checking')}</div>}
                  {mode.checks && Object.keys(mode.checks).length === 0 && <div className="mu">{t('editor.connect.noTargets')}</div>}
                  <ul className="ed-targets">
                    {Object.entries(mode.checks ?? {}).map(([to, q]) => (
                      <li key={to}>
                        <Button
                          size="sm"
                          disabled={!q.ok}
                          data-testid={`connect-to-${to}`}
                          onClick={() =>
                            void connect(mode.from!, to).then((r) => {
                              if (r.ok) setMode(null)
                            })
                          }
                        >
                          <span className="mono">{to}</span>
                        </Button>
                        {!q.ok && <span className="mu"> {q.message}</span>}
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </div>
          )}
        </div>
      )}

      {def && !empty && (
        <>
          <ViewControls onArrange={arrange} arrangeDisabled={readOnly} minimap={minimap} onMinimap={() => setMinimap((m) => !m)} />
          <Dock
            tool={tool}
            onTool={setTool}
            nodes={Object.entries(def.nodes).map(([id, n]) => ({ id, type: n.type }))}
            selected={selectedId}
            onPick={(id) => {
              setSel({ kind: 'node', id })
              const n = flow.getNode(id)
              if (n) void flow.setCenter(n.position.x + (n.measured?.width ?? 264) / 2, n.position.y + (n.measured?.height ?? 120) / 2, { zoom: flow.getZoom(), duration: 300 })
            }}
            onAdd={addNode}
            readOnly={readOnly}
          />
        </>
      )}

      <StepMenu
        open={menu}
        onClose={() => setMenu(null)}
        readOnly={readOnly}
        hasOutputs={!!menu && portNames(def?.nodes[menu.id]?.outputs).length > 0}
        onConnect={() => setMode({ kind: 'connect' })}
        onRename={startRename}
        onDelete={startDelete}
      />
    </div>
  )
}

function placementsAuto(def: Definition): Record<string, Point> {
  // Auto-layout writes the deterministic layout for every node, replacing stored positions.
  const auto = placements({ ...def, metadata: undefined })
  return Object.fromEntries(Object.entries(auto).map(([id, p]) => [id, { x: p.x, y: p.y }]))
}

// fitView only runs once, when React Flow mounts; the container can still be
// resizing then (view switch). Refit whenever its size changes.
function FitOnResize({ target }: { target: React.RefObject<HTMLDivElement | null> }) {
  const { fitView } = useReactFlow()
  // Cards have no fixed size: React Flow measures them, and only then can the view fit them.
  const measured = useNodesInitialized()
  useEffect(() => {
    if (measured) void fitView({ padding: 0.2, maxZoom: 1 })
  }, [measured, fitView])
  useEffect(() => {
    const el = target.current
    if (!el || !measured) return
    let frame = 0
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => void fitView({ padding: 0.2, maxZoom: 1 }))
    })
    ro.observe(el)
    return () => {
      ro.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [fitView, target, measured])
  return null
}

