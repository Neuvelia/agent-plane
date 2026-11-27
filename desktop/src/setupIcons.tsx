import type { ReactNode } from 'react'
import type { RowTone } from './setup'

// Stroke glyphs of the setup screens, drawn as on the canvas. All are
// decorative: the node kind or status is always written next to them.

const NODE_PATHS: Record<string, { stroke: string; d: ReactNode }> = {
  shell: { stroke: 'var(--fg)', d: <path d="M2 3l3 3-3 3M6.5 9H10" strokeLinecap="round" /> },
  agent: { stroke: 'var(--brand)', d: <circle cx="6" cy="6" r="4" /> },
  gate: { stroke: 'var(--fg)', d: <path d="M6 1.5 10.5 6 6 10.5 1.5 6z" strokeLinejoin="round" /> },
  human: {
    stroke: 'var(--fg)',
    d: (
      <>
        <circle cx="6" cy="4" r="1.8" />
        <path d="M2.5 10c.5-2 2-3 3.5-3s3 1 3.5 3" strokeLinecap="round" />
      </>
    ),
  },
}

/** The glyph of a node kind; an unknown kind draws the gate's neutral diamond. */
export function NodeGlyph({ kind, size = 18 }: { kind: string; size?: number }) {
  const g = NODE_PATHS[kind] ?? NODE_PATHS.gate!
  return (
    <svg width={size} height={size} viewBox="0 0 12 12" fill="none" stroke={g.stroke} strokeWidth="1.3" aria-hidden="true" focusable="false">
      {g.d}
    </svg>
  )
}

const line = { fill: 'none', stroke: 'currentColor', strokeLinecap: 'round', strokeLinejoin: 'round' } as const

export function Tick({ size = 16, width = 2.6, color = 'currentColor' }: { size?: number; width?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" {...line} stroke={color} strokeWidth={width} aria-hidden="true" focusable="false">
      <path d="M4 12.5 9 17.5 20 6.5" />
    </svg>
  )
}

export function Arrow() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" {...line} strokeWidth="2.2" aria-hidden="true" focusable="false">
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  )
}

export function Monitor({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" {...line} strokeWidth="2" aria-hidden="true" focusable="false">
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </svg>
  )
}

export function Lock() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" {...line} strokeWidth="2" aria-hidden="true" focusable="false">
      <rect x="5" y="11" width="14" height="9" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </svg>
  )
}

export function Prompt() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" {...line} strokeWidth="2" aria-hidden="true" focusable="false">
      <path d="m5 8 4 4-4 4M12 16h7" />
    </svg>
  )
}

/** A status shape per tone, so status never relies on colour alone. */
export function StatusGlyph({ tone, size = 20 }: { tone: RowTone; size?: number }) {
  if (tone === 'ok') return <Tick size={size} color="var(--ok)" />
  if (tone === 'warn')
    return (
      <svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true" focusable="false">
        <path d="M6 1 11.2 10.5H.8z" fill="var(--warn)" />
      </svg>
    )
  if (tone === 'bad')
    return (
      <svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true" focusable="false">
        <circle cx="6" cy="6" r="5.5" fill="var(--bad)" />
        <path d="M6 3.2v3.4M6 8.6v.1" stroke="var(--bg)" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    )
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="var(--muted)" strokeWidth="2" strokeLinecap="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="9" />
      <path d="M8 12h8" />
    </svg>
  )
}
