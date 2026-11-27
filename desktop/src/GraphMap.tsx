import dagre from '@dagrejs/dagre'
import { Background, Handle, Position, ReactFlow, useReactFlow, useStore, type Edge, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { createContext, useContext, useEffect, useMemo } from 'react'
import { useI18n } from './i18n/context'
import { displayStatus, nodeView, type RunView } from './reducer'
import { NodePill } from './status'
import type { GraphSummary } from './types'
import { GRID_DOT, useResolvedTheme } from './theme'

const W = 250
const H = 76

// Node objects are STATIC (id, kind, position, size). The run state and the
// selection reach the node component through this context instead of node.data:
// replacing node objects on every event batch makes React Flow drop its handle
// measurements, and then it silently draws no edges.
// `critical` and `dimmed` (F2.6) are node IDs: on the critical path, and hidden by the node filter.
interface MapState {
  run: RunView
  selected: string | null
  critical: ReadonlySet<string>
  dimmed: ReadonlySet<string>
}
const NONE: ReadonlySet<string> = new Set()
const StateCtx = createContext<MapState>({ run: { status: 'running', nodeCount: 0, nodes: {}, order: [], lastSequence: 0, eventCount: 0, tail: [] }, selected: null, critical: NONE, dimmed: NONE })

interface ApNodeData extends Record<string, unknown> {
  id: string
  kind: string
}

function ApNode({ data }: NodeProps<Node<ApNodeData>>) {
  const { t } = useI18n()
  const { run, selected, critical, dimmed } = useContext(StateCtx)
  const n = nodeView(run, data.id)
  const status = displayStatus(n)
  const last = n.attempts[n.attempts.length - 1]
  const label = status === 'retrying' && last ? t('status.node.retryingOf', { n: last.number, max: last.max }) : undefined
  return (
    <div className={`ap-node s-${status}${selected === data.id ? ' sel' : ''}${dimmed.has(data.id) ? ' dim' : ''}`} data-testid={`node-${data.id}`} data-status={status} data-dimmed={dimmed.has(data.id) || undefined} data-critical={critical.has(data.id) || undefined}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="ap-node-top">
        <b>{data.id}</b>
        <span className="tag">{data.kind}</span>
        {critical.has(data.id) && <span className="tag" data-testid={`critical-${data.id}`}>{t('overview.criticalBadge')}</span>}
        {dimmed.has(data.id) && <span className="tag">{t('nodes.dimmed')}</span>}
      </div>
      <div className="ap-node-bottom">
        <NodePill status={status} label={label} />
        {n.attempts.length > 1 && <span className="tag">{t('node.attemptCount', { count: n.attempts.length })}</span>}
        {n.durationMs !== undefined && <span className="mu mono">{n.durationMs} ms</span>}
      </div>
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  )
}

const nodeTypes = { ap: ApNode }

// fitView only runs once, when React Flow mounts; the container can still be
// resizing then (cards, view switch). Refit whenever the container's size changes.
function FitOnResize() {
  const { fitView } = useReactFlow()
  // The flow's own element: a view switch can replace the wrapper around it, never this one.
  const el = useStore((s) => s.domNode)
  useEffect(() => {
    if (!el) return
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
  }, [fitView, el])
  return null
}

// Layout comes from the graph definition only (dagre); node status comes from
// the event reduction only. Neither is stored in this component.
export function GraphMap({ graph, run, selected, onSelect, critical = NONE, dimmed = NONE }: { graph: GraphSummary; run: RunView; selected: string | null; onSelect: (id: string) => void; critical?: ReadonlySet<string>; dimmed?: ReadonlySet<string> }) {
  const theme = useResolvedTheme()
  const positions = useMemo(() => {
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: 'LR', ranksep: 70, nodesep: 28 })
    g.setDefaultEdgeLabel(() => ({}))
    graph.nodes.forEach((n) => g.setNode(n.id, { width: W, height: H }))
    graph.edges.forEach((e) => g.setEdge(e.from, e.to))
    dagre.layout(g)
    return Object.fromEntries(graph.nodes.map((n) => [n.id, g.node(n.id)]))
  }, [graph])

  // Only the graph (never run state or selection) may change these objects.
  const nodes = useMemo<Node<ApNodeData>[]>(
    () =>
      graph.nodes.map((n) => ({
        id: n.id,
        type: 'ap',
        position: { x: (positions[n.id]?.x ?? 0) - W / 2, y: (positions[n.id]?.y ?? 0) - H / 2 },
        data: { id: n.id, kind: n.type },
        // Fixed, known size, so a node is visible without waiting for a measurement.
        width: W,
        height: H,
        draggable: false,
        connectable: false,
      })),
    [graph, positions],
  )
  const edges = useMemo<Edge[]>(
    () =>
      graph.edges.map((e) => {
        const from = nodeView(run, e.from).status
        const to = displayStatus(nodeView(run, e.to))
        const flowing = from === 'succeeded' && (to === 'running' || to === 'retrying' || to === 'waiting_human')
        return { id: `${e.from}->${e.to}`, source: e.from, target: e.to, animated: flowing, className: flowing ? 'flow' : '' }
      }),
    [graph, run],
  )

  return (
    <div className="map" data-testid="graph-map">
      <StateCtx.Provider value={{ run, selected, critical, dimmed }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
        // A narrow pane (Split view) must still show the whole graph.
        minZoom={0.2}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => onSelect(node.id)}
        colorMode={theme}
      >
        <Background gap={22} color={GRID_DOT[theme]} />
        <FitOnResize />
      </ReactFlow>
      </StateCtx.Provider>
    </div>
  )
}
