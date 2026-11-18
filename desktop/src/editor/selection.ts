import { useCallback, useEffect, useState } from 'react'
import type { Target } from './issues'
import type { Definition } from './types'

// The editor's one selection (F2.3 E5a.2). The canvas, the node list, the
// Bindings panel and the inspectors all read and write it here, so choosing a
// row selects the edge on the canvas and the reverse. It holds a choice, never a
// graph fact: a selection that is not in the definition any more is dropped.

/** A node, an edge between nodes, or a graph input binding (`inputs.NAME` to a node input). */
export type Selection = { kind: 'node'; id: string } | { kind: 'edge'; from: string; to: string } | { kind: 'binding'; from: string; to: string } | null

export interface SelectionOwner {
  selection: Selection
  select: (next: Selection) => void
}

const has = (list: { from: string; to: string }[] | undefined, s: { from: string; to: string }) => (list ?? []).some((e) => e.from === s.from && e.to === s.to)

/** The selection if the definition still holds it, otherwise null. */
export function pruneSelection(sel: Selection, def: Definition | undefined): Selection {
  if (!sel || !def) return null
  if (sel.kind === 'node') return sel.id in def.nodes ? sel : null
  return has(sel.kind === 'edge' ? def.edges : def.graph_input_bindings, sel) ? sel : null
}

/** What choosing an issue selects. A port selects its node; the canvas outlines the port itself. */
export function selectionFor(target: Target): Selection {
  switch (target.kind) {
    case 'node':
      return { kind: 'node', id: target.id }
    case 'port':
      return { kind: 'node', id: target.node }
    case 'edge':
      return { kind: 'edge', from: target.from, to: target.to }
    case 'binding':
      return { kind: 'binding', from: target.from, to: target.to }
    case 'graph':
      return null
  }
}

/** Selection state for one open document; `scope` (the grant) changes when another document replaces it. */
export function useSelection(def: Definition | undefined, scope: string): SelectionOwner {
  const [state, setState] = useState<{ scope: string; selection: Selection }>({ scope, selection: null })
  const select = useCallback((selection: Selection) => setState({ scope, selection }), [scope])
  useEffect(() => {
    setState((s) => {
      const next = s.scope === scope ? pruneSelection(s.selection, def) : null
      return next === s.selection && s.scope === scope ? s : { scope, selection: next }
    })
  }, [def, scope])
  return { selection: state.scope === scope ? state.selection : null, select }
}
