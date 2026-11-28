import dagre from '@dagrejs/dagre'
import type { Diagnostic } from '../types'
import type { Definition, NodeDef } from './types'

// Presentation helpers for the canvas. They read the definition the engine
// returned and decide where things are DRAWN. No graph rule lives here: whether
// two ports may be connected is always answered by the engine (can_connect).

/** Step card widths (graph editor v2): wider when the card shows a prompt. Heights are estimates for layout; the canvas measures real cards. */
export const NODE_W = 236
const WIDE_W = 266
const HEAD_H = 56
const ROW_H = 28
const PROMPT_H = 56

export interface Point {
  x: number
  y: number
}

export const NODE_TYPES = ['shell', 'agent', 'gate', 'human'] as const

/** Estimated card size: one row per input, one per output, one for a loop, one for a key setting, and a prompt box. */
export function nodeSize(n: NodeDef): { w: number; h: number } {
  const rows = Object.keys(n.inputs ?? {}).length + Math.max(Object.keys(n.outputs ?? {}).length, 1) + (n.loop ? 1 : 0) + 1
  const prompt = Object.values(n.config ?? {}).some((v) => typeof v === 'string' && v.length > 48) ? PROMPT_H : 0
  return { w: prompt ? WIDE_W : NODE_W, h: HEAD_H + prompt + rows * ROW_H }
}

export interface Ref {
  node: string
  side: 'inputs' | 'outputs'
  port: string
}

/** Splits `NODE.outputs.PORT` or `NODE.inputs.PORT`. Node ids never contain a dot. */
export function parseRef(r: string): Ref | null {
  const m = /^([^.]+)\.(inputs|outputs)\.(.+)$/.exec(r)
  return m ? { node: m[1]!, side: m[2] as Ref['side'], port: m[3]! } : null
}

export const makeRef = (node: string, side: Ref['side'], port: string) => `${node}.${side}.${port}`

const LIMIT = 100000

/**
 * Positions stored in `metadata.layout.nodes` (ADR-0022). Reading is tolerant: a
 * malformed layout, an unknown node or a non-integer coordinate is ignored for
 * that node and never reported.
 */
export function readLayout(def: Definition): Record<string, Point> {
  const out: Record<string, Point> = {}
  const nodes = (def.metadata?.layout as { nodes?: unknown } | undefined)?.nodes
  if (!nodes || typeof nodes !== 'object') return out
  for (const [id, v] of Object.entries(nodes as Record<string, unknown>)) {
    if (!(id in def.nodes) || !v || typeof v !== 'object') continue
    const { x, y } = v as { x?: unknown; y?: unknown }
    const ok = (n: unknown): n is number => typeof n === 'number' && Number.isInteger(n) && Math.abs(n) <= LIMIT
    if (ok(x) && ok(y)) out[id] = { x, y }
  }
  return out
}

/** Deterministic left-to-right placement of every node (top-left corners). */
export function autoLayout(def: Definition): Record<string, Point> {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', ranksep: 56, nodesep: 56 })
  g.setDefaultEdgeLabel(() => ({}))
  const ids = Object.keys(def.nodes).sort()
  for (const id of ids) {
    const s = nodeSize(def.nodes[id]!)
    g.setNode(id, { width: s.w, height: s.h })
  }
  for (const e of def.edges) {
    const a = parseRef(e.from)
    const b = parseRef(e.to)
    if (a && b && a.node in def.nodes && b.node in def.nodes) g.setEdge(a.node, b.node)
  }
  dagre.layout(g)
  const out: Record<string, Point> = {}
  for (const id of ids) {
    const n = g.node(id)
    const s = nodeSize(def.nodes[id]!)
    out[id] = { x: Math.round(n.x - s.w / 2), y: Math.round(n.y - s.h / 2) }
  }
  return out
}

/** Stored position when there is one, otherwise the automatic one. */
export function placements(def: Definition): Record<string, Point> {
  const auto = autoLayout(def)
  const stored = readLayout(def)
  return Object.fromEntries(Object.keys(def.nodes).map((id) => [id, stored[id] ?? auto[id] ?? { x: 0, y: 0 }]))
}

/** `shell_1`, `shell_2`, ...: the lowest number not in use (F2.1, open question 1). */
export function suggestId(type: string, existing: string[]): string {
  const taken = new Set(existing)
  for (let i = 1; ; i++) if (!taken.has(`${type}_${i}`)) return `${type}_${i}`
}

/** What deleting a node takes with it, for the confirmation text. */
export function removalImpact(def: Definition, id: string): { edges: number; bindings: number; outputs: number } {
  const touches = (r: string) => parseRef(r)?.node === id
  return {
    edges: def.edges.filter((e) => touches(e.from) || touches(e.to)).length,
    bindings: (def.graph_input_bindings ?? []).filter((e) => touches(e.to)).length,
    outputs: Object.values(def.outputs).filter((o) => touches(o.from)).length,
  }
}

export function diagnosticCounts(diags: Diagnostic[]): Record<string, { errors: number; warnings: number }> {
  const out: Record<string, { errors: number; warnings: number }> = {}
  for (const d of diags) {
    const id = d.location?.node_id
    if (!id) continue
    const c = (out[id] ??= { errors: 0, warnings: 0 })
    if (d.severity === 'error') c.errors++
    else if (d.severity === 'warning') c.warnings++
  }
  return out
}


export interface Rect extends Point {
  w: number
  h: number
}

const GAP = 24

/**
 * A spot near `centre` (top-left corner) where a card of `size` covers no card in
 * `taken` (with a small gap), stepping diagonally; gives up after a bounded search
 * and returns the last spot tried.
 */
export function clearSpot(centre: Point, taken: Rect[], size: { w: number; h: number }): Point {
  let p = { x: Math.round(centre.x - size.w / 2), y: Math.round(centre.y - size.h / 2) }
  const hits = (q: Point) => taken.some((r) => q.x < r.x + r.w + GAP && r.x < q.x + size.w + GAP && q.y < r.y + r.h + GAP && r.y < q.y + size.h + GAP)
  for (let i = 0; i < 200 && hits(p); i++) p = { x: p.x + 32, y: p.y + 32 }
  return p
}
