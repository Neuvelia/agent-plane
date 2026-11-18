import {
  Background,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
  type OnConnectStart,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Card, Pill, Tag } from '../ui/primitives'
import { queryConnectTargets } from './connect'
import type { DocState } from './doc'
import { NODE_TYPES, freeSpot, makeRef, nodeSize, parseRef, placements, removalImpact, rowY, suggestId, type Point } from './layout'
import { badgesOf, type Badge, type Badges, type FocusReq } from './issues'
import type { Selection, SelectionOwner } from './selection'
import type { Definition, Op, QueryResult } from './types'
import type { GraphDocument } from './useGraphDocument'
import { Inspector } from './inspector/Inspector'

// The canvas draws the definition the engine returned and turns gestures into
// ADR-0020 operations. Whether a connection is allowed is the engine's answer
// (can_connect); nothing here decides it. Node objects are static: selection,
// diagnostics and connect hints reach them through context, because replacing
// node objects makes React Flow drop its handle measurements and draw no edges.

interface PortData {
  name: string
  type: string
  required: boolean
}

interface EdData extends Record<string, unknown> {
  id: string
  kind: string
  inputs: PortData[]
  outputs: PortData[]
  sig: string
}

interface CanvasState {
  selectedNode: string | null
  badges: Badges
  /** The port an issue pointed at, outlined until something else is selected. */
  focusPort: string | null
  readOnly: boolean
  /** While a connection is being dragged: the engine's verdict per target input. */
  reasons: Record<string, QueryResult>
}

const Ctx = createContext<CanvasState>({ selectedNode: null, badges: { nodes: {}, edges: {}, ports: {} }, focusPort: null, readOnly: true, reasons: {} })

const portList = (ports: Record<string, { type: string; required?: boolean }> | undefined): PortData[] =>
  Object.entries(ports ?? {}).map(([name, p]) => ({ name, type: p.type, required: p.required === true }))

function EdNode({ data }: NodeProps<Node<EdData>>) {
  const { t } = useI18n()
  const c = useContext(Ctx)
  const size = nodeSize({ type: data.kind, inputs: Object.fromEntries(data.inputs.map((p) => [p.name, p])), outputs: Object.fromEntries(data.outputs.map((p) => [p.name, p])) })
  const n = c.badges.nodes[data.id]
  return (
    <div className={`ed-node${c.selectedNode === data.id ? ' sel' : ''}`} style={{ width: size.w, height: size.h }} data-testid={`node-${data.id}`}>
      <div className="ed-node-head">
        <b>{data.id}</b>
        <span className="row" style={{ gap: 6, flexWrap: 'nowrap' }}>
          {n && n.errors > 0 && <Pill tone="x" small>{'✖ '}{t('editor.nodeErrors', { count: n.errors })}</Pill>}
          {n && n.warnings > 0 && <Pill tone="w" small>{'⚠ '}{t('editor.nodeWarnings', { count: n.warnings })}</Pill>}
          <span className="tag">{data.kind}</span>
        </span>
      </div>
      {data.inputs.map((p, i) => {
        const ref = makeRef(data.id, 'inputs', p.name)
        const verdict = c.reasons[ref]
        const bad = verdict !== undefined && !verdict.ok
        const badge = c.badges.ports[ref]
        return (
          <div className={`ed-port in${c.focusPort === ref ? ' focus' : ''}`} style={{ top: rowY(i) - 11 }} key={`in:${p.name}`}>
            <Handle type="target" position={Position.Left} id={`in:${p.name}`} style={{ top: rowY(i) }} isConnectable={!c.readOnly} className={bad ? 'ed-bad' : verdict?.ok ? 'ed-ok' : ''} title={bad ? verdict.message : undefined} data-testid={`handle-${ref}`} />
            <span className="mono">{p.name}</span>
            <span className="mu">{p.type}</span>
            <PortBadge badge={badge} />
          </div>
        )
      })}
      {data.outputs.map((p, i) => (
        <div className={`ed-port out${c.focusPort === makeRef(data.id, 'outputs', p.name) ? ' focus' : ''}`} style={{ top: rowY(i) - 11 }} key={`out:${p.name}`}>
          <PortBadge badge={c.badges.ports[makeRef(data.id, 'outputs', p.name)]} />
          <span className="mu">{p.type}</span>
          <span className="mono">{p.name}</span>
          <Handle type="source" position={Position.Right} id={`out:${p.name}`} style={{ top: rowY(i) }} isConnectable={!c.readOnly} data-testid={`handle-${makeRef(data.id, 'outputs', p.name)}`} />
        </div>
      ))}
    </div>
  )
}

// A port with an issue says so in words for screen readers and in a glyph for everyone; never colour alone.
function PortBadge({ badge }: { badge?: Badge }) {
  const { t } = useI18n()
  if (!badge) return null
  const err = badge.errors > 0
  return (
    <span className={`ed-port-badge ${err ? 'err' : 'warn'}`} role="img" aria-label={err ? t('editor.nodeErrors', { count: badge.errors }) : t('editor.nodeWarnings', { count: badge.warnings })}>
      {err ? '✖' : '⚠'}
    </span>
  )
}

const nodeTypes = { ed: EdNode }

function desiredNodes(def: Definition, pos: Record<string, Point>): Node<EdData>[] {
  return Object.keys(def.nodes)
    .sort()
    .map((id) => {
      const n = def.nodes[id]!
      const inputs = portList(n.inputs)
      const outputs = portList(n.outputs)
      return {
        id,
        type: 'ed',
        position: pos[id] ?? { x: 0, y: 0 },
        data: { id, kind: n.type, inputs, outputs, sig: JSON.stringify([n.type, inputs, outputs]) },
        width: nodeSize(n).w,
        height: nodeSize(n).h,
        connectable: true,
      }
    })
}

/** Keeps node objects that did not change, so React Flow keeps their measurements. */
function mergeNodes(prev: Node<EdData>[], next: Node<EdData>[]): Node<EdData>[] {
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

export function GraphCanvas(props: { g: GraphDocument; doc: DocState; focusReq: FocusReq | null; selection: SelectionOwner }) {
  return (
    <ReactFlowProvider>
      <Inner {...props} />
    </ReactFlowProvider>
  )
}

function Inner({ g, doc, focusReq, selection }: { g: GraphDocument; doc: DocState; focusReq: FocusReq | null; selection: SelectionOwner }) {
  const { t } = useI18n()
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
  const [nodes, setNodes] = useState<Node<EdData>[]>([])
  const [resetKey, setResetKey] = useState(0)
  const dragging = useRef(false)

  const desired = useMemo(() => (def ? desiredNodes(def, placements(def)) : []), [def])
  useEffect(() => {
    if (dragging.current) return
    setNodes((prev) => mergeNodes(prev, desired))
  }, [desired, resetKey])

  const badges = useMemo(() => badgesOf(doc.analysis.diagnostics, def), [doc.analysis.diagnostics, def])
  const [focusPort, setFocusPort] = useState<string | null>(null)
  const [focusTick, setFocusTick] = useState(0)
  const ctx = useMemo<CanvasState>(() => ({ selectedNode: sel?.kind === 'node' ? sel.id : null, badges, focusPort, readOnly, reasons }), [sel, badges, focusPort, readOnly, reasons])
  const graphHead = useRef<HTMLDivElement>(null)
  const selectedHead = useRef<HTMLElement>(null)

  // An issue was chosen (the editor already selected its element): outline its port, then move keyboard focus to its heading.
  useEffect(() => {
    if (!focusReq || !def) return
    const t = focusReq.target
    setFocusPort(t.kind === 'port' ? makeRef(t.node, t.side, t.port) : t.kind === 'binding' ? t.to : null)
    setFocusTick(focusReq.nonce) // focus once the selection below has rendered
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusReq?.nonce])
  useEffect(() => {
    if (!focusReq || focusTick === 0) return
    ;(focusReq.target.kind === 'graph' ? graphHead.current : selectedHead.current)?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTick])

  const edges = useMemo<Edge[]>(
    () =>
      (def?.edges ?? []).flatMap((e) => {
        const a = parseRef(e.from)
        const b = parseRef(e.to)
        if (!a || !b) return []
        const selected = sel?.kind === 'edge' && sel.from === e.from && sel.to === e.to
        const badge = badges.edges[`${e.from} -> ${e.to}`]
        const label = badge ? (badge.errors > 0 ? `✖ ${t('editor.nodeErrors', { count: badge.errors })}` : `⚠ ${t('editor.nodeWarnings', { count: badge.warnings })}`) : undefined
        return [{ id: `${e.from}->${e.to}`, source: a.node, target: b.node, sourceHandle: `out:${a.port}`, targetHandle: `in:${b.port}`, selected, ariaLabel: `${e.from} → ${e.to}`, className: selected ? 'ed-edge-sel' : '', label, labelStyle: { fill: 'var(--fg)', fontSize: 11 }, labelBgStyle: { fill: 'var(--raised)' } }]
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
    const el = document.querySelector('[data-testid="editor-canvas"]')
    const box = el?.getBoundingClientRect()
    const centre = box ? flow.screenToFlowPosition({ x: box.left + box.width / 2, y: box.top + box.height / 2 }) : { x: 0, y: 0 }
    const spot = freeSpot(centre, nodes.map((n) => n.position))
    const id = suggestId(type, Object.keys(def.nodes))
    void run([{ op: 'add_node', id, type }, { op: 'set_layout', nodes: { [id]: spot } }]).then((r) => r.ok && setSel({ kind: 'node', id }))
  }

  const arrange = () => {
    if (!def) return
    const all = placementsAuto(def)
    void run([{ op: 'set_layout', nodes: all }])
  }

  const move = (id: string, p: Point) => void run([{ op: 'set_layout', nodes: { [id]: { x: Math.round(p.x), y: Math.round(p.y) } } }])

  const connect = (from: string, to: string) => run([{ op: 'add_edge', from, to }])

  const queryTargets = useCallback(
    async (from: string): Promise<Record<string, QueryResult>> => {
      if (!def) return {}
      const targets = Object.entries(def.nodes).flatMap(([id, n]) => Object.keys(n.inputs ?? {}).map((p) => makeRef(id, 'inputs', p)))
      const a = await queryConnectTargets((ops) => g.applyOps(ops, { quiet: true }), from, targets)
      if (a.ok) return a.targets
      setNotice(t('editor.connectTargetsFailed', { message: a.message }))
      return {}
    },
    [def, g, t],
  )

  const onConnectStart: OnConnectStart = (_, p) => {
    if (p.handleType !== 'source' || !p.nodeId || !p.handleId?.startsWith('out:')) return
    const from = makeRef(p.nodeId, 'outputs', p.handleId.slice(4))
    setReasons({})
    void queryTargets(from).then(setReasons)
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
    if (state.isValid === false && state.toHandle?.id?.startsWith('in:')) {
      const to = makeRef(state.toHandle.nodeId, 'inputs', state.toHandle.id.slice(3))
      const why = reasonsRef.current[to]?.message
      if (why) setNotice(t('editor.cannotConnect', { message: why }))
    }
    setReasons({})
  }

  const onNodesChange = (changes: NodeChange<Node<EdData>>[]) => setNodes((n) => applyNodeChanges(changes, n))

  const selectedNode = sel?.kind === 'node' && def ? def.nodes[sel.id] : undefined
  const selectedId = sel?.kind === 'node' ? sel.id : null
  const impact = def && selectedId ? removalImpact(def, selectedId) : null

  const startConnect = () => {
    setMode({ kind: 'connect' })
  }
  const pickOutput = async (port: string) => {
    if (!selectedId) return
    const from = makeRef(selectedId, 'outputs', port)
    setMode({ kind: 'connect', from })
    const checks = await queryTargets(from)
    setMode((m) => (m && m.kind === 'connect' && m.from === from ? { kind: 'connect', from, checks } : m))
  }

  return (
    <div className="ed-visual" data-testid="canvas-card">
      <Card
        title={t('editor.canvas')}
        testId="canvas"
        action={
          <span className="row" role="toolbar" aria-label={t('editor.canvasTools')}>
            {NODE_TYPES.map((type) => (
              <Button key={type} size="sm" onClick={() => addNode(type)} disabled={readOnly} data-testid={`add-${type}`}>
                {t('editor.addNode', { type: t(`editor.type.${type}`) })}
              </Button>
            ))}
            <Button size="sm" onClick={startConnect} disabled={readOnly || !selectedNode || portList(selectedNode.outputs).length === 0} data-testid="op-connect">
              {t('editor.connect')}
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setRenameTo(selectedId ?? '')
                setMode({ kind: 'rename' })
              }}
              disabled={readOnly || !selectedId}
              data-testid="op-rename"
            >
              {t('editor.rename')}
            </Button>
            <Button size="sm" variant="danger" onClick={() => (impact && impact.edges + impact.bindings + impact.outputs > 0 ? setMode({ kind: 'delete' }) : selectedId && void run([{ op: 'remove_node', id: selectedId }]))} disabled={readOnly || !selectedId} data-testid="op-delete">
              {t('editor.delete')}
            </Button>
            <Button size="sm" onClick={arrange} disabled={readOnly || Object.keys(def?.nodes ?? {}).length === 0} data-testid="op-arrange">
              {t('editor.arrange')}
            </Button>
            <Button size="sm" onClick={g.revalidate} data-testid="op-validate">
              {t('editor.validate')}
            </Button>
          </span>
        }
      >
        {block && (
          <div className="banner warn" role="status" data-testid="canvas-readonly">
            <span>{block === 'parse' ? t('editor.readonly.parse', { message: doc.analysis.parseError ?? '' }) : t('editor.readonly.anchors')}</span>
          </div>
        )}
        {notice && (
          <div className="banner warn" role="status" data-testid="canvas-notice">
            <span>{notice}</span>
            <Button size="sm" onClick={() => setNotice(null)}>
              {t('editor.dismiss')}
            </Button>
          </div>
        )}
        {mode?.kind === 'rename' && selectedId && (
          <form
            className="row ed-strip"
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
          >
            <label htmlFor="rename-input">{t('editor.rename.label', { node: selectedId })}</label>
            <input id="rename-input" className="input mono" value={renameTo} onChange={(e) => setRenameTo(e.target.value)} autoFocus data-testid="rename-input" />
            <Button size="sm" variant="primary" type="submit" data-testid="rename-apply">
              {t('editor.rename.apply')}
            </Button>
            <Button size="sm" onClick={() => setMode(null)}>
              {t('editor.cancel')}
            </Button>
          </form>
        )}
        {mode?.kind === 'delete' && selectedId && impact && (
          <div className="row ed-strip" role="alertdialog" aria-label={t('editor.delete')} data-testid="delete-confirm">
            <span>{t('editor.delete.confirm', { node: selectedId, edges: impact.edges, bindings: impact.bindings, outputs: impact.outputs })}</span>
            <Button
              size="sm"
              variant="danger"
              data-testid="delete-apply"
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
          <div className="ed-strip" data-testid="connect-panel">
            {!mode.from ? (
              <div className="row">
                <span>{t('editor.connect.pickOutput', { node: selectedId })}</span>
                {portList(selectedNode.outputs).map((p) => (
                  <Button key={p.name} size="sm" onClick={() => void pickOutput(p.name)} data-testid={`connect-out-${p.name}`}>
                    {p.name} <span className="mu">{p.type}</span>
                  </Button>
                ))}
                <Button size="sm" onClick={() => setMode(null)}>
                  {t('editor.cancel')}
                </Button>
              </div>
            ) : (
              <>
                <div className="row">
                  <span>{t('editor.connect.pickTarget', { from: mode.from })}</span>
                  <Button size="sm" onClick={() => setMode(null)}>
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
        {def && (
          <div className="ed-graph-head" tabIndex={-1} ref={graphHead} data-testid="graph-heading">
            <b>{def.name ?? def.id}</b>
          </div>
        )}
        <div className="ed-flow" data-testid="editor-canvas" role="region" aria-label={t('editor.canvasLabel')}>
          {def && Object.keys(def.nodes).length === 0 && <div className="ed-empty mu">{t('editor.canvas.emptyGraph')}</div>}
          {!def && <div className="ed-empty mu">{t('editor.canvas.noGraph')}</div>}
          <Ctx.Provider value={ctx}>
            <ReactFlow
              nodes={nodes}
              edges={edges}
              nodeTypes={nodeTypes}
              onNodesChange={onNodesChange}
              fitView
              fitViewOptions={{ padding: 0.2 }}
              nodesDraggable={!readOnly}
              nodesConnectable={!readOnly}
              edgesFocusable
              deleteKeyCode={null}
              proOptions={{ hideAttribution: true }}
              onNodeClick={(_, n) => setSel({ kind: 'node', id: n.id })}
              onEdgeClick={(_, e) => {
                const a = e.sourceHandle?.startsWith('out:') ? makeRef(e.source, 'outputs', e.sourceHandle.slice(4)) : ''
                const b = e.targetHandle?.startsWith('in:') ? makeRef(e.target, 'inputs', e.targetHandle.slice(3)) : ''
                setSel({ kind: 'edge', from: a, to: b })
              }}
              onPaneClick={() => setSel(null)}
              onNodeDragStart={() => (dragging.current = true)}
              onNodeDragStop={(_, n) => {
                dragging.current = false
                move(n.id, n.position)
              }}
              onConnectStart={onConnectStart}
              onConnect={onConnect}
              onConnectEnd={onConnectEnd}
              isValidConnection={isValidConnection}
              colorMode="dark"
            >
              <Background gap={22} color="#242424" />
              <FitOnResize />
            </ReactFlow>
          </Ctx.Provider>
        </div>
      </Card>

      <Card title={t('editor.nodes')} testId="node-list-card">
        {def && Object.keys(def.nodes).length === 0 && <div className="mu empty-line">{t('editor.nodesEmpty')}</div>}
        <ul className="ed-list" aria-label={t('editor.nodes')}>
          {Object.entries(def?.nodes ?? {}).map(([id, n]) => (
            <li key={id}>
              <button type="button" className={`ed-list-item${selectedId === id ? ' sel' : ''}`} aria-pressed={selectedId === id} onClick={() => setSel({ kind: 'node', id })} data-testid={`list-node-${id}`}>
                <b className="mono">{id}</b>
                <Tag>{n.type}</Tag>
              </button>
            </li>
          ))}
        </ul>
        {def && sel && (
          <Selected headRef={selectedHead} def={def} sel={sel} onRemoveEdge={(from, to) => void run([{ op: 'remove_edge', from, to }]).then((r) => r.ok && sel.kind === 'edge' && setSel(null))} readOnly={readOnly} />
        )}
      </Card>

      <Inspector g={g} doc={doc} nodeId={selectedId} readOnly={readOnly} onRenamed={(id) => setSel({ kind: 'node', id })} />
    </div>
  )
}

function placementsAuto(def: Definition): Record<string, Point> {
  // Arrange writes the deterministic layout for every node, replacing stored positions.
  const auto = placements({ ...def, metadata: undefined })
  return Object.fromEntries(Object.entries(auto).map(([id, p]) => [id, { x: p.x, y: p.y }]))
}

function Selected({ def, sel, onRemoveEdge, readOnly, headRef }: { def: Definition; sel: NonNullable<Selection>; onRemoveEdge: (from: string, to: string) => void; readOnly: boolean; headRef: React.RefObject<HTMLElement | null> }) {
  const { t } = useI18n()
  const conns = sel.kind === 'node' ? def.edges.filter((e) => parseRef(e.from)?.node === sel.id || parseRef(e.to)?.node === sel.id) : def.edges.filter((e) => e.from === sel.from && e.to === sel.to)
  const node = sel.kind === 'node' ? def.nodes[sel.id] : undefined
  return (
    <div className="ed-selected" data-testid="selected">
      <b className="ed-selected-head mono" tabIndex={-1} ref={headRef as React.RefObject<HTMLElement>} data-testid="selected-heading">
        {sel.kind === 'node' ? sel.id : `${sel.from} → ${sel.to}`}
      </b>
      {sel.kind === 'node' && node && (
        <>
          <div className="row">
            <Tag>{node.type}</Tag>
          </div>
          <div className="mu">{t('editor.ports.inputs')}: {portList(node.inputs).map((p) => `${p.name}: ${p.type}${p.required ? '' : '?'}`).join(', ') || t('editor.ports.none')}</div>
          <div className="mu">{t('editor.ports.outputs')}: {portList(node.outputs).map((p) => `${p.name}: ${p.type}`).join(', ') || t('editor.ports.none')}</div>
        </>
      )}
      <div className="mu">{sel.kind === 'node' ? t('editor.connections') : t('editor.connection')}</div>
      {conns.length === 0 && <div className="mu">{t('editor.connections.none')}</div>}
      {conns.map((e) => (
        <div className="row between" key={`${e.from}->${e.to}`} data-testid="connection">
          <span className="mono">
            {e.from} → {e.to}
          </span>
          <Button size="sm" variant="danger" disabled={readOnly} onClick={() => onRemoveEdge(e.from, e.to)} aria-label={t('editor.connection.removeLabel', { from: e.from, to: e.to })} data-testid="remove-edge">
            {t('editor.connection.remove')}
          </Button>
        </div>
      ))}
    </div>
  )
}

// fitView only runs once, when React Flow mounts; the container can still be
// resizing then (view switch). Refit whenever its size changes.
function FitOnResize() {
  const { fitView } = useReactFlow()
  useEffect(() => {
    const el = document.querySelector('[data-testid="editor-canvas"]')
    if (!el) return
    let frame = 0
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => void fitView({ padding: 0.2 }))
    })
    ro.observe(el)
    return () => {
      ro.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [fitView])
  return null
}
