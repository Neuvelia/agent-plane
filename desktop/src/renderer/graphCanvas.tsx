import { useEffect, useRef, useState } from 'react'
import type { PointerEvent, WheelEvent } from 'react'
import type { GraphInspection, GraphLayout, GraphProjection } from './graphDocument'

const NODE_WIDTH = 180
const NODE_HEIGHT = 88
const POSITION_BOUND = 100_000

export function clampCanvasZoom(value: number): number {
  return Number.isFinite(value) ? Math.max(0.5, Math.min(2, value)) : 1
}

export function movedCanvasPosition(position: { x: number; y: number }, dx: number, dy: number, zoom: number): { x: number; y: number } {
  const scale = clampCanvasZoom(zoom)
  return {
    x: Math.max(-POSITION_BOUND, Math.min(POSITION_BOUND, Math.round(position.x + dx / scale))),
    y: Math.max(-POSITION_BOUND, Math.min(POSITION_BOUND, Math.round(position.y + dy / scale)))
  }
}

export type CanvasEdge = { from: string; to: string; x1: number; y1: number; x2: number; y2: number }

export function graphEdgeEndpoints(graph: GraphProjection, layout: GraphLayout): CanvasEdge[] {
  const nodes = new Set(graph.nodes.map((node) => node.id))
  const result: CanvasEdge[] = []
  for (const edge of graph.edges) {
    const from = edge.from.match(/^([^.]*)\.outputs\.([^.]*)$/)
    const to = edge.to.match(/^([^.]*)\.inputs\.([^.]*)$/)
    if (!from || !to || !nodes.has(from[1]) || !nodes.has(to[1])) continue
    const start = layout.nodes[from[1]]
    const end = layout.nodes[to[1]]
    if (!start || !end) continue
    result.push({ from: edge.from, to: edge.to,
      x1: start.x + NODE_WIDTH, y1: start.y + NODE_HEIGHT / 2,
      x2: end.x, y2: end.y + NODE_HEIGHT / 2 })
  }
  return result
}

type Props = {
  inspection: GraphInspection
  selectedID: string | null
  disabled: boolean
  stale?: boolean
  onSelect(id: string): void
  onDelete(id: string): void
  onMove(id: string, x: number, y: number): void
  onResetLayout(): void
}

type Drag =
  | { kind: 'node'; pointerID: number; nodeID: string; startX: number; startY: number; position: { x: number; y: number } }
  | { kind: 'pan'; pointerID: number; startX: number; startY: number; panX: number; panY: number }

export function GraphCanvas({ inspection, selectedID, disabled, stale = false, onSelect, onDelete, onMove, onResetLayout }: Props): React.JSX.Element {
  const graph = inspection.graph
  const layout = inspection.layout
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [moving, setMoving] = useState<{ id: string; x: number; y: number } | null>(null)
  const drag = useRef<Drag | null>(null)
  const nodeRefs = useRef(new Map<string, HTMLButtonElement>())
  useEffect(() => { setMoving(null) }, [inspection.revision])

  const positioned = layout ? { ...layout, nodes: { ...layout.nodes } } : null
  if (positioned && moving) positioned.nodes[moving.id] = { x: moving.x, y: moving.y }
  const edges = graph && positioned ? graphEdgeEndpoints(graph, positioned) : []

  function beginNodeDrag(event: PointerEvent<HTMLButtonElement>, id: string): void {
    if (disabled || !layout?.nodes[id]) return
    event.stopPropagation()
    onSelect(id)
    drag.current = { kind: 'node', pointerID: event.pointerId, nodeID: id,
      startX: event.clientX, startY: event.clientY, position: layout.nodes[id] }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function moveNode(event: PointerEvent<HTMLButtonElement>): void {
    const active = drag.current
    if (!active || active.kind !== 'node' || active.pointerID !== event.pointerId) return
    const next = movedCanvasPosition(active.position, event.clientX - active.startX, event.clientY - active.startY, zoom)
    setMoving({ id: active.nodeID, ...next })
  }

  function finishNodeDrag(event: PointerEvent<HTMLButtonElement>): void {
    const active = drag.current
    if (!active || active.kind !== 'node' || active.pointerID !== event.pointerId) return
    drag.current = null
    const next = movedCanvasPosition(active.position, event.clientX - active.startX, event.clientY - active.startY, zoom)
    if (next.x !== active.position.x || next.y !== active.position.y) onMove(active.nodeID, next.x, next.y)
    setMoving(null)
  }

  function beginPan(event: PointerEvent<HTMLDivElement>): void {
    drag.current = { kind: 'pan', pointerID: event.pointerId, startX: event.clientX, startY: event.clientY,
      panX: pan.x, panY: pan.y }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function updatePan(event: PointerEvent<HTMLDivElement>): void {
    const active = drag.current
    if (!active || active.kind !== 'pan' || active.pointerID !== event.pointerId) return
    setPan({ x: active.panX + event.clientX - active.startX, y: active.panY + event.clientY - active.startY })
  }

  function wheel(event: WheelEvent<HTMLDivElement>): void {
    event.preventDefault()
    setZoom((current) => clampCanvasZoom(current * (event.deltaY < 0 ? 1.1 : 1 / 1.1)))
  }

  function keyNode(event: React.KeyboardEvent<HTMLButtonElement>, id: string): void {
    if (disabled || !graph) return
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault()
      onDelete(id)
      return
    }
    if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return
    event.preventDefault()
    const index = graph.nodes.findIndex((node) => node.id === id)
    const next = graph.nodes[(index + (event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 1) + graph.nodes.length) % graph.nodes.length]
    if (next) { onSelect(next.id); nodeRefs.current.get(next.id)?.focus() }
  }

  return <section className="graph-canvas-shell" aria-label="Graph canvas">
    {(stale || inspection.validity !== 'valid') && <div className="graph-canvas-notice" role="status">Stale canvas · correct YAML before visual editing</div>}
    {layout?.source === 'unsupported_version' && <div className="graph-canvas-notice" role="status">Unknown layout version · review replacement before moving nodes</div>}
    <div className="graph-canvas" aria-label="Graph nodes and dependencies" aria-disabled={disabled}
      onPointerDown={beginPan} onPointerMove={updatePan} onPointerUp={() => { if (drag.current?.kind === 'pan') drag.current = null }}
      onPointerCancel={() => { drag.current = null; setMoving(null) }} onWheel={wheel}>
      <div className="graph-canvas-world" style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}>
        <svg className="graph-canvas-edges" aria-label="Data dependencies" role="img">
          {edges.map((edge) => <path key={`${edge.from}->${edge.to}`} data-from={edge.from} data-to={edge.to}
            d={`M ${edge.x1} ${edge.y1} C ${edge.x1 + 42} ${edge.y1}, ${edge.x2 - 42} ${edge.y2}, ${edge.x2} ${edge.y2}`}>
            <title>{edge.from} → {edge.to}</title>
          </path>)}
        </svg>
        <div role="listbox" aria-label="Graph nodes">
          {graph?.nodes.map((node) => {
            const position = positioned?.nodes[node.id] ?? { x: 0, y: 0 }
            return <button key={node.id} type="button" role="option" aria-selected={selectedID === node.id}
              aria-label={`${node.type} node ${node.id}${node.needs_configuration ? ', needs configuration' : ''}`}
              className={`graph-canvas-node graph-canvas-node-${node.type}${selectedID === node.id ? ' selected' : ''}`}
              style={{ transform: `translate(${position.x}px, ${position.y}px)` }} disabled={disabled}
              ref={(element) => { if (element) nodeRefs.current.set(node.id, element); else nodeRefs.current.delete(node.id) }}
              onClick={() => onSelect(node.id)} onKeyDown={(event) => keyNode(event, node.id)}
              onPointerDown={(event) => beginNodeDrag(event, node.id)} onPointerMove={moveNode}
              onPointerUp={finishNodeDrag} onPointerCancel={() => { drag.current = null; setMoving(null) }}>
              <span className="graph-canvas-node-type">{node.type} · {node.type === 'agent' ? 'Codex' : 'local'}</span>
              <strong>{node.id}</strong>
              {node.needs_configuration && <span className="graph-canvas-needs">Needs configuration</span>}
              <span className="graph-canvas-ports">
                {node.inputs.map((port) => <small key={`in:${port.name}`}>{port.name} · {port.type}</small>)}
                {node.outputs.map((port) => <small key={`out:${port.name}`}>{port.name} · {port.type}</small>)}
              </span>
            </button>
          })}
        </div>
      </div>
    </div>
    <div className="graph-canvas-controls">
      <button type="button" aria-label="Zoom out" onClick={() => setZoom((value) => clampCanvasZoom(value / 1.1))}>−</button>
      <output aria-label="Canvas zoom">{Math.round(zoom * 100)}%</output>
      <button type="button" aria-label="Zoom in" onClick={() => setZoom((value) => clampCanvasZoom(value * 1.1))}>+</button>
      {disabled ? null : <button type="button" onClick={onResetLayout}>Reset layout</button>}
    </div>
  </section>
}
