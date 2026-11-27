import type { ReactNode } from 'react'
import type { NodeStatus } from '../reducer'

// Small 12px status shapes for dense rows (F2.5b). Each shape differs, not only
// its colour, and is decorative: the row always carries the status in words.
export type GlyphKind = 'ok' | 'bad' | 'warn' | 'paused' | 'approval' | 'running' | 'idle'

const SHAPES: Record<GlyphKind, ReactNode> = {
  ok: <path d="M2 6.5 5 9.5 10 3" fill="none" stroke="var(--ok)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />,
  bad: <path d="M2.5 2.5l7 7M9.5 2.5l-7 7" fill="none" stroke="var(--bad)" strokeWidth="1.8" strokeLinecap="round" />,
  paused: (
    <>
      <rect x="2.5" y="2" width="2.4" height="8" rx=".8" fill="var(--warn)" />
      <rect x="7.1" y="2" width="2.4" height="8" rx=".8" fill="var(--warn)" />
    </>
  ),
  // A warning that does not block (F2.5c preflight): a triangle, not the approval diamond.
  warn: <path d="M6 1 11.2 10.5H.8z" fill="var(--warn)" />,
  approval: <path d="M6 1 11 6 6 11 1 6z" fill="var(--warn)" />,
  running: (
    <g className="glyph-spin">
      <circle cx="6" cy="6" r="4.5" fill="none" stroke="var(--brand)" strokeOpacity=".3" strokeWidth="1.6" />
      <path d="M6 1.5a4.5 4.5 0 0 1 4.5 4.5" fill="none" stroke="var(--brand)" strokeWidth="1.6" strokeLinecap="round" />
    </g>
  ),
  idle: <circle cx="6" cy="6" r="2.5" fill="none" stroke="var(--muted)" strokeWidth="1.4" />,
}

export function Glyph({ kind }: { kind: GlyphKind }) {
  return (
    <svg className="glyph" width="12" height="12" viewBox="0 0 12 12" aria-hidden="true" focusable="false">
      {SHAPES[kind]}
    </svg>
  )
}

const NODE_GLYPH: Record<NodeStatus, GlyphKind> = {
  pending: 'idle',
  ready: 'idle',
  running: 'running',
  retrying: 'running',
  waiting_human: 'approval',
  succeeded: 'ok',
  failed: 'bad',
  skipped: 'idle',
  cancelled: 'idle',
  interrupted: 'paused',
}

export const nodeGlyph = (status: NodeStatus): GlyphKind => NODE_GLYPH[status]
