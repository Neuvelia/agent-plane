import dagre from '@dagrejs/dagre'
import { Background, Handle, Position, ReactFlow, useReactFlow, type Edge, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useEffect, useMemo } from 'react'
import { useI18n } from './i18n/context'
import { displayStatus, nodeView, type RunView } from './reducer'
import { NodePill } from './status'
import type { GraphSummary } from './types'

const W = 250
const H = 76

interface ApNodeData extends Record<string, unknown> {
  id: string
  kind: string
  run: RunView
  selected: boolean
}

function ApNode({ data }: NodeProps<Node<ApNodeData>>) {
  const { t } = useI18n()
  const n = nodeView(data.run, data.id)
  const status = displayStatus(n)
  const last = n.attempts[n.attempts.length - 1]
  const label = status === 'retrying' && last ? t('status.node.retryingOf', { n: last.number, max: last.max }) : undefined
  return (
    <div className={`ap-node s-${status}${data.selected ? ' sel' : ''}`} data-testid={`node-${data.id}`} data-status={status}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="ap-node-top">
        <b>{data.id}</b>
        <span className="tag">{data.kind}</span>
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
  useEffect(() => {
    const el = document.querySelector('[data-testid="graph-map"]')
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

// Layout comes from the graph definition only (dagre); node status comes from
// the event reduction only. Neither is stored in this component.
export function GraphMap({ graph, run, selected, onSelect }: { graph: GraphSummary; run: RunView; selected: string | null; onSelect: (id: string) => void }) {
  const positions = useMemo(() => {
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: 'LR', ranksep: 70, nodesep: 28 })
    g.setDefaultEdgeLabel(() => ({}))
    graph.nodes.forEach((n) => g.setNode(n.id, { width: W, height: H }))
    graph.edges.forEach((e) => g.setEdge(e.from, e.to))
    dagre.layout(g)
    return Object.fromEntries(graph.nodes.map((n) => [n.id, g.node(n.id)]))
  }, [graph])

  // Rebuilding every node object on each 30 ms event batch makes React Flow
  // re-adopt and re-measure the whole graph. Recompute only when the graph, the
  // reduced run state or the selection actually changed.
  const nodes = useMemo<Node<ApNodeData>[]>(
    () =>
      graph.nodes.map((n) => ({
        id: n.id,
        type: 'ap',
        position: { x: (positions[n.id]?.x ?? 0) - W / 2, y: (positions[n.id]?.y ?? 0) - H / 2 },
        data: { id: n.id, kind: n.type, run, selected: selected === n.id },
        // Fixed, known size. React Flow keeps a node hidden until it has measured
        // it; we replace the node objects on every event batch, which can drop the
        // measurement, and ResizeObserver will not fire again for an unchanged
        // size. Declaring the size makes the node visible without measuring.
        width: W,
        height: H,
        draggable: false,
        connectable: false,
      })),
    [graph, positions, run, selected],
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
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => onSelect(node.id)}
        colorMode="dark"
      >
        <Background gap={22} color="#242424" />
        <FitOnResize />
      </ReactFlow>
    </div>
  )
}
